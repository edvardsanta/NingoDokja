package handlers

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	chat "read_books/dokja_domain/dokja_chat"
	digest "read_books/dokja_domain/dokja_digest"
	promptfiles "read_books/dokja_store/prompts"
	"read_books/internal/core"
	"read_books/internal/logger"
	"regexp"
	"strings"
	"sync"
	"time"
)

const webReadBudget = 15 * time.Second

// webSearchBudget bounds one search for a chat turn; a slow instance never holds the reply.
const webSearchBudget = 10 * time.Second
const webCacheSessions = 64
const webCacheTTL = 30 * time.Minute

// socialReadBudget bounds the post readers for one chat link. The local reader answers in
// seconds; a slow or dead reader falls back to the plain web reader instead of holding the turn.
const socialReadBudget = 10 * time.Second

// socialMediaBudget bounds delivering and reading a post's media for one chat link.
// It matches the image budget: a GPU model answers in seconds, a cold one needs most of this.
const socialMediaBudget = 60 * time.Second

// maxPostMediaReadings bounds how many of one post's media are read for a chat turn.
const maxPostMediaReadings = 2

// SocialMediaVision describes media a post reader delivered. Implemented by the vision
// service client; nil leaves posts as caption-only.
type SocialMediaVision interface {
	DescribeMedia(ctx context.Context, dataB64, contentType, filename string) (string, error)
}

type socialMediaReading struct {
	describe SocialMediaVision
	enabled  func() bool
}

// Only this fixed application text is a system message. Page titles, dates,
// URLs, headings and text are serialized together as an untrusted user message.
func webConversationBrief() string { return promptfiles.MustRead("web_conversation_brief") }

type webFailure struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}
type webTurnData struct {
	FailureOnly bool         `json:"-"`
	Failures    []webFailure `json:"failures,omitempty"`
	// ServedStored lists URLs whose fresh re-read failed and whose passages came from
	// a copy kept from an earlier read in this conversation; the content may be outdated.
	ServedStored []string `json:"served_stored,omitempty"`
	// Deferred lists URLs whose fresh read failed transiently, with no kept copy,
	// and whose reading the application promised to retry in the background and
	// deliver as a follow-up.
	Deferred []string `json:"deferred,omitempty"`
	Omitted  int      `json:"omitted_links,omitempty"`
}
type webSessionReading struct {
	id        string
	seen      time.Time
	documents []chat.WebDocument
	// searches keeps what a search answered for one query, so follow-ups on the same subject
	// reuse it instead of searching again. It lives and dies with the readings it complements.
	searches map[string]webSearchAnswer
}

// webSearchAnswer is what one search returned, as sources ready to number.
type webSearchAnswer struct {
	results     []webSearchResult
	retrievedAt string
}
type webSearchResult struct{ title, url, snippet string }

// maxWebSearchQueries bounds the searches one conversation keeps, so a long chat cannot grow it.
const maxWebSearchQueries = 8

type webConversationReading struct {
	domain   *digest.Domain
	enabled  func() bool
	mu       sync.Mutex
	sessions map[string]webSessionReading
	now      func() time.Time
}

func (h *ChatDomainHandler) WithWebReading(service digest.Service, enabled func() bool) *ChatDomainHandler {
	h.web = &webConversationReading{domain: digest.New(service), enabled: enabled, sessions: map[string]webSessionReading{}, now: time.Now}
	return h
}

// upsert folds a document a background task read into the session's kept set,
// replacing the entry of its own URL and keeping the set capped. A session that
// moved on (or never had this reading) is left alone: a deferred follow-up must
// not resurrect readings the conversation already dropped.
func (w *webConversationReading) upsert(key, sessionID string, document chat.WebDocument) {
	w.mu.Lock()
	defer w.mu.Unlock()
	saved := w.sessions[key]
	if saved.id == "" || saved.id != sessionID {
		return
	}
	for i := range saved.documents {
		if saved.documents[i].URL == document.URL {
			saved.documents[i] = document
			w.sessions[key] = saved
			return
		}
	}
	saved.documents = append(saved.documents, document)
	if len(saved.documents) > chat.MaxWebLinks {
		saved.documents = saved.documents[len(saved.documents)-chat.MaxWebLinks:]
	}
	w.sessions[key] = saved
}

// WithSocialMediaVision lets chat read media a post reader delivered: a reel's frames, a
// photo's text. The vision service describes bytes the delivery already holds.
func (h *ChatDomainHandler) WithSocialMediaVision(describe SocialMediaVision, enabled func() bool) *ChatDomainHandler {
	h.socialVision = &socialMediaReading{describe: describe, enabled: enabled}
	return h
}

func (h *ChatDomainHandler) webSources(ctx context.Context, event core.Event, sessionID, question string) ([]knowledgeSource, *webTurnData) {
	if (event.Source != core.SourceDiscord && event.Source != core.SourceWhatsApp) || h.web == nil {
		return nil, nil
	}
	w := h.web
	attachments, _ := event.Payload["document_attachments"].([]chat.WebAttachment)
	links := chat.PlanWebReadingsWithLimit(question, attachments, chat.PDFLimitBytes(os.Getenv("DOKJA_PDF_MAX_MIB")))
	key := chatSessionBaseKey(event)
	if key == "" {
		return nil, nil
	}
	now := w.now()
	w.mu.Lock()
	for k, s := range w.sessions {
		if now.Sub(s.seen) > webCacheTTL {
			delete(w.sessions, k)
		}
	}
	saved := w.sessions[key]
	if saved.id != sessionID {
		delete(w.sessions, key)
		saved = webSessionReading{}
	}
	w.mu.Unlock()
	if len(links) == 0 && len(saved.documents) == 0 {
		saved = webSessionReading{id: sessionID, documents: h.loadReadingContext(ctx, key, sessionID)}
	}
	// An explicit research ask ("pesquisa ...") is a request for something new: it searches
	// whatever the conversation already holds, and the readings kept from before stay out of it.
	_, explicitResearch := chat.WebSearchPlan(nil, question)
	researchOnly := explicitResearch && len(links) == 0
	if !researchOnly {
		if len(links) == 0 && len(saved.documents) == 0 {
			return nil, nil
		}
		// Kept readings follow the conversation only while the message is about them; otherwise
		// a page shared long ago is attached to every later message, noise included.
		if len(links) == 0 && !h.readingRelevant(ctx, question, saved.documents) {
			return nil, nil
		}
	}
	data := &webTurnData{}
	if w.enabled != nil && !w.enabled() {
		for _, link := range links {
			data.Failures = append(data.Failures, webFailure{link.URL, "reader_disabled"})
		}
		return nil, data
	}
	if len(links) > 0 {
		data.Omitted = max(0, len(links)-chat.MaxWebLinks)
		links = links[:min(len(links), chat.MaxWebLinks)]
		readCtx, cancel := context.WithTimeout(ctx, webReadBudget)
		defer cancel()
		if len(saved.documents) == 0 {
			saved.documents = h.loadReadingContext(ctx, key, sessionID)
		}
		// A new read updates its own URL; documents read earlier in this session stay
		// available. Resetting to an empty set here used to drop every previous reading
		// on a transient fetch failure — and then the save below wiped the durable copy.
		stored := make(map[string]chat.WebDocument, len(saved.documents))
		merged := make([]chat.WebDocument, 0, len(saved.documents)+len(links))
		for _, document := range saved.documents {
			if _, dup := stored[document.URL]; !dup {
				stored[document.URL] = document
				merged = append(merged, document)
			}
		}
		replace := func(document chat.WebDocument) {
			for i := range merged {
				if merged[i].URL == document.URL {
					merged[i] = document
					return
				}
			}
			merged = append(merged, document)
		}
		for _, link := range links {
			if link.Error != "" {
				if _, ok := stored[link.URL]; ok {
					data.ServedStored = append(data.ServedStored, link.URL)
				} else {
					data.Failures = append(data.Failures, webFailure{link.URL, link.Error})
				}
				continue
			}
			result, kind, err := readChatLink(readCtx, w.domain, link.URL)
			if err != nil {
				if _, ok := stored[link.URL]; ok {
					// The fresh read failed but an earlier read of this same URL is kept:
					// serve that copy, marked as possibly outdated in the turn notice.
					// A served copy never defers: the speaker already has something.
					data.ServedStored = append(data.ServedStored, link.URL)
					continue
				}
				reason := digest.WebFailureReason(err)
				data.Failures = append(data.Failures, webFailure{link.URL, reason})
				// A transient failure may settle later: the turn promises a follow-up
				// and a background re-read comes back to the channel (ADR 0017).
				if event.Source == core.SourceDiscord && h.proactive != nil && chat.DeferredReadingReason(reason) {
					data.Deferred = append(data.Deferred, link.URL)
					h.deferReading(ctx, event, link.URL, question)
				}
				continue
			}
			document, err := decodeWebDocument(result)
			if err != nil {
				if _, ok := stored[link.URL]; ok {
					data.ServedStored = append(data.ServedStored, link.URL)
					continue
				}
				data.Failures = append(data.Failures, webFailure{link.URL, "unreadable"})
				continue
			}
			if kind == "post" {
				h.appendPostMediaReadings(readCtx, w.domain, link.URL, &document)
			}
			h.learnFromReading(readCtx, event, kind, map[string]any{"url": document.URL}, func(storeCtx context.Context) {
				h.knowledgePersist.persistWebReading(storeCtx, event, document)
			})
			replace(document)
		}
		if len(merged) > chat.MaxWebLinks {
			merged = merged[len(merged)-chat.MaxWebLinks:]
		}
		saved = webSessionReading{id: sessionID, documents: merged}
	}
	if len(links) > 0 {
		h.saveReadingContext(ctx, key, sessionID, saved.documents)
	}
	research, researched := h.researchContext(ctx, &saved, question, researchOnly)
	saved.seen = now
	w.mu.Lock()
	if len(w.sessions) >= webCacheSessions {
		oldest := ""
		var at time.Time
		for k, s := range w.sessions {
			if oldest == "" || s.seen.Before(at) {
				oldest, at = k, s.seen
			}
		}
		delete(w.sessions, oldest)
	}
	w.sessions[key] = saved
	w.mu.Unlock()
	var sources []knowledgeSource
	readings := saved.documents
	if researchOnly {
		readings = nil
	}
	for _, document := range chat.SelectWebPassages(readings, question) {
		hash := sha256.Sum256([]byte(document.URL))
		id := "web:" + hex.EncodeToString(hash[:12])
		for _, passage := range document.Passages {
			sources = append(sources, knowledgeSource{
				Number: len(sources) + 1, SourceID: id, Title: document.Title, Heading: passage.Label,
				Text: passage.Text, TextVersion: document.TextVersion, Reference: passage.URL, Web: true,
				Published: document.Published, RetrievedAt: document.RetrievedAt, Partial: document.Partial,
			})
		}
	}
	if researched {
		for _, result := range research.results {
			hash := sha256.Sum256([]byte(result.url))
			sources = append(sources, knowledgeSource{
				Number: len(sources) + 1, SourceID: "search:" + hex.EncodeToString(hash[:12]), Title: result.title,
				Heading: webSearchHeading, Text: result.snippet, Reference: result.url, Web: true,
				RetrievedAt: research.retrievedAt, Partial: true,
			})
			// The owner's searches store directly; anyone else's are held as
			// pending proposals the owner can approve into references.
			h.learnFromSearch(ctx, event, result, research.retrievedAt)
		}
	}
	return sources, data
}

// webSearchHeading marks a passage as a search snippet, never a page the person shared.
const webSearchHeading = "search result"

// researchContext searches the web when the readings so far cannot answer the question, using
// the reading's title as the context a bare name lacks. A search that fails, finds nothing or is
// not configured leaves the turn exactly as it was.
func (h *ChatDomainHandler) researchContext(ctx context.Context, saved *webSessionReading, question string, researchOnly bool) (webSearchAnswer, bool) {
	query, ok := chat.WebSearchPlan(saved.documents, question)
	if !ok {
		logger.Info("web search skipped reason=not_asked_or_covered_by_reading")
		return webSearchAnswer{}, false
	}
	logger.Info(fmt.Sprintf("web search planned query_words=%d explicit=%t", len(strings.Fields(query)), researchOnly))
	if answer, cached := saved.searches[query]; cached {
		return answer, len(answer.results) > 0
	}
	searchCtx, cancel := context.WithTimeout(ctx, webSearchBudget)
	defer cancel()
	reply, err := h.web.domain.SearchWeb(searchCtx, query, "")
	if err != nil {
		logger.Info(fmt.Sprintf("web search failed reason=%s", digest.WebFailureReason(err)))
		return webSearchAnswer{}, false
	}
	answer := webSearchAnswer{}
	answer.retrievedAt, _ = reply["retrieved_at"].(string)
	items, _ := reply["results"].([]map[string]any)
	if items == nil {
		if generic, ok := reply["results"].([]any); ok {
			for _, item := range generic {
				if entry, ok := item.(map[string]any); ok {
					items = append(items, entry)
				}
			}
		}
	}
	for _, item := range items {
		title, _ := item["title"].(string)
		address, _ := item["url"].(string)
		snippet, _ := item["snippet"].(string)
		if address != "" && snippet != "" {
			answer.results = append(answer.results, webSearchResult{title, address, snippet})
		}
	}
	if saved.searches == nil || len(saved.searches) >= maxWebSearchQueries {
		saved.searches = map[string]webSearchAnswer{}
	}
	saved.searches[query] = answer
	return answer, len(answer.results) > 0
}

// readChatLink reads one user link for the conversation. The social post readers go first —
// they see what a plain fetch cannot, like a reel behind Instagram's login wall — and any
// refusal or failure falls back to the plain web reader, so a setup without readers behaves
// exactly as before. Media is not downloaded in a chat turn.
func readChatLink(ctx context.Context, domain *digest.Domain, address string) (map[string]any, string, error) {
	if document, ok := socialPostReading(ctx, domain, address); ok {
		return document, "post", nil
	}
	result, err := domain.ReadWebLink(ctx, address)
	return result, "web_article", err
}

// socialPostReading asks the enabled post readers for one link and shapes an accepted post
// as a web-reading document: author and caption only, so media addresses from a reader's
// local worker never reach the model or the chat. Refusals, failures and posts with nothing
// to discuss report false so the caller falls back to the web reader.
func socialPostReading(ctx context.Context, domain *digest.Domain, address string) (map[string]any, bool) {
	if domain == nil {
		return nil, false
	}
	socialCtx, cancel := context.WithTimeout(ctx, socialReadBudget)
	defer cancel()
	reply, err := domain.Handle(socialCtx, digest.Request{Action: digest.ActionReadLink, Event: digest.Event{Payload: map[string]any{"url": address}}})
	if err != nil {
		logger.Info(fmt.Sprintf("social reader failed link=%s: %v", address, err))
		return nil, false
	}
	if reply["status"] != "ok" {
		logger.Info(fmt.Sprintf("social reader refused link=%s reason=%v", address, reply["refused"]))
		return nil, false
	}
	post, _ := reply["post"].(map[string]any)
	canonical, _ := post["url"].(string)
	if canonical == "" {
		canonical = address
	}
	author := strings.TrimSpace(fmt.Sprint(post["author"]))
	caption := strings.TrimSpace(fmt.Sprint(post["caption"]))
	text := caption
	if text == "" {
		text = author
	}
	if text == "" {
		return nil, false
	}
	title := author
	if title == "" {
		title = "post"
	}
	passages := []map[string]any{{"label": "post", "text": text, "url": canonical}}
	if caption != "" && author != "" {
		passages = append(passages, map[string]any{"label": "author", "text": author, "url": canonical})
	}
	return map[string]any{
		"url": canonical, "title": title, "published": fmt.Sprint(post["published"]),
		"passages": passages,
	}, true
}

// appendPostMediaReadings delivers a read post's media and appends the vision readings as
// passages of its document. Any failure keeps the caption-only document: media is a
// bonus on top of the reading, never a reason to drop the turn. Streamed media tokens
// are not reassembled here; the inline bytes a reader already produced are enough.
func (h *ChatDomainHandler) appendPostMediaReadings(ctx context.Context, domain *digest.Domain, link string, document *chat.WebDocument) {
	if h.socialVision == nil || h.socialVision.describe == nil ||
		(h.socialVision.enabled != nil && !h.socialVision.enabled()) {
		return
	}
	mediaCtx, cancel := context.WithTimeout(ctx, socialMediaBudget)
	defer cancel()
	reply, err := domain.Handle(mediaCtx, digest.Request{Action: digest.ActionReadLinkDelivery, Event: digest.Event{Payload: map[string]any{"url": link}}})
	if err != nil || reply["status"] != "ok" {
		logger.Info(fmt.Sprintf("social media delivery failed link=%s err=%v refused=%v", link, err, reply["refused"]))
		return
	}
	rows, _ := reply["media"].([]digest.DeliveryMedia)
	described := 0
	for _, item := range rows {
		if described == maxPostMediaReadings {
			break
		}
		if item.DataB64 == "" {
			continue
		}
		text, err := h.socialVision.describe.DescribeMedia(mediaCtx, item.DataB64, item.ContentType, item.Filename)
		if err != nil || strings.TrimSpace(text) == "" {
			logger.Info(fmt.Sprintf("social media reading failed link=%s media=%s err=%v", link, item.Filename, err))
			continue
		}
		label := "video"
		if strings.HasPrefix(strings.ToLower(item.ContentType), "image/") {
			label = "image"
		}
		document.Passages = append(document.Passages, chat.WebPassage{Label: label, Text: boundWebText(text, 2400), URL: document.URL})
		described++
	}
	if described > 0 {
		document.TextVersion = webDocumentTextVersion(*document)
	}
}

func decodeWebDocument(result map[string]any) (chat.WebDocument, error) {
	var d chat.WebDocument
	// Bound the service response before decoding/caching it. Page values never
	// become application instructions, markup, local paths or future fetch input.
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 1<<20 {
		return d, fmt.Errorf("invalid reading")
	}
	if err = json.Unmarshal(encoded, &d); err != nil {
		return d, err
	}
	base, err := url.Parse(d.URL)
	if err != nil || base.Hostname() == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") || len(d.Passages) == 0 || len(d.Passages) > 2000 {
		return d, fmt.Errorf("invalid reading")
	}
	d.Title = boundWebText(d.Title, 300)
	d.Published = boundWebText(d.Published, 100)
	d.RetrievedAt = boundWebText(d.RetrievedAt, 100)
	remaining := 80000
	for i := range d.Passages {
		p := &d.Passages[i]
		address, err := url.Parse(p.URL)
		if err != nil || address.Scheme != base.Scheme || address.Host != base.Host || address.Path != base.Path || address.RawQuery != base.RawQuery || address.User != nil {
			return d, fmt.Errorf("invalid passage address")
		}
		p.Label = boundWebText(p.Label, 200)
		if len([]rune(p.Text)) > min(2400, remaining) {
			d.Partial = true
		}
		p.Text = boundWebText(p.Text, min(2400, remaining))
		remaining -= len([]rune(p.Text))
		if remaining == 0 {
			d.Passages = d.Passages[:i+1]
			d.Partial = true
			break
		}
	}
	d.TextVersion = webDocumentTextVersion(d)
	return d, nil
}

func webDocumentTextVersion(document chat.WebDocument) string {
	var text strings.Builder
	for _, passage := range document.Passages {
		text.WriteString(passage.Label)
		text.WriteByte(0)
		text.WriteString(passage.Text)
		text.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(text.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func boundWebText(s string, n int) string {
	r := []rune(strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), ""))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func withWebEvidence(messages []map[string]string, sources []knowledgeSource, data *webTurnData) []map[string]string {
	var passages []map[string]any
	for _, s := range sources {
		if s.Web {
			passages = append(passages, map[string]any{"number": s.Number, "title": s.Title, "section": s.Heading, "text": s.Text, "text_version": s.TextVersion, "url": s.Reference, "published": s.Published, "retrieved_at": s.RetrievedAt, "partial": s.Partial})
		}
	}
	if len(passages) == 0 && data == nil {
		return messages
	}
	if data == nil {
		data = &webTurnData{}
	}
	encoded, _ := json.Marshal(map[string]any{"kind": "UNTRUSTED_WEB_EVIDENCE", "passages": passages, "reading_status": data})
	last := max(0, len(messages)-1)
	out := make([]map[string]string, 0, len(messages)+2)
	out = append(out, messages[:last]...)
	out = append(out, map[string]string{"role": "system", "content": webConversationBrief()}, map[string]string{"role": "user", "content": string(encoded)})
	return append(out, messages[last:]...)
}

var webMention = regexp.MustCompile(`<@[!&]?[0-9]+>`)

var generatedWebURL = regexp.MustCompile(`(?i)https?://[^\s<>\[\]()"']+`)
var generatedWebLink = regexp.MustCompile(`(?i)\[([^\]\n]+)\]\(https?://[^\s)]+\)`)

// Do not render model-generated destinations from untrusted page instructions.
// Valid source links are appended from the service's validated passage records.
func sanitizeWebReply(reply string, sources []knowledgeSource) string {
	reply = webMention.ReplaceAllStringFunc(reply, func(s string) string { return strings.ReplaceAll(s, "@", "＠") })
	reply = strings.NewReplacer("@everyone", "＠everyone", "@here", "＠here").Replace(reply)
	reply = generatedWebLink.ReplaceAllString(reply, "$1")
	reply = generatedWebURL.ReplaceAllStringFunc(reply, func(address string) string {
		for _, s := range sources {
			if s.Web && s.Reference == strings.TrimRight(address, ".,;") {
				return fmt.Sprintf("[%d]", s.Number)
			}
		}
		return quoteRemoved
	})
	valid := map[string]bool{}
	for _, s := range sources {
		valid[fmt.Sprint(s.Number)] = true
	}
	return citationPattern.ReplaceAllStringFunc(reply, func(mark string) string {
		var kept []string
		for _, n := range strings.Split(strings.Trim(mark, "[]"), ",") {
			n = strings.TrimSpace(n)
			if valid[n] {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			return quoteRemoved
		}
		return "[" + strings.Join(kept, ", ") + "]"
	})
}

func webSourceLabel(source knowledgeSource) string {
	label := strings.Join(strings.Fields(source.Title+" · "+source.Heading), " ")
	label = generatedWebURL.ReplaceAllString(label, quoteRemoved)
	label = strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "*", "", "_", "", "`", "", "@", "＠", "<", "", ">", "").Replace(label)
	u, err := url.Parse(source.Reference)
	if err != nil {
		return label
	}
	// Angle brackets keep source-owned punctuation out of Markdown syntax.
	address := strings.NewReplacer("<", "%3C", ">", "%3E").Replace(u.String())
	return fmt.Sprintf("[%d] %s <%s>", source.Number, label, address)
}

//go:embed locales/web_reading_*.json
var webReadingLocales embed.FS

var webReadingCatalogs = func() map[string]selfChatCatalog {
	catalogs := map[string]selfChatCatalog{}
	for _, locale := range []string{"pt", "en"} {
		raw, err := webReadingLocales.ReadFile("locales/web_reading_" + locale + ".json")
		if err != nil {
			panic(err)
		}
		catalog := selfChatCatalog{}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			panic(err)
		}
		catalogs[locale] = catalog
	}
	return catalogs
}()

func webReadingStatus(event core.Event, data *webTurnData) string {
	if data == nil {
		return ""
	}
	locale, _ := event.Payload["locale"].(string)
	if !strings.HasPrefix(locale, "en") {
		locale = "pt"
	} else {
		locale = "en"
	}
	catalog := webReadingCatalogs[locale]
	var lines []string
	blocked, pdfUnreadable, tooLarge := 0, 0, 0
	for _, failure := range data.Failures {
		if failure.Reason == "blocked" {
			blocked++
		}
		if failure.Reason == "pdf_unreadable" {
			pdfUnreadable++
		}
		if failure.Reason == "too_large" {
			tooLarge++
		}
	}
	if blocked > 0 {
		key := "blocked_many"
		if blocked == 1 {
			key = "blocked_one"
		}
		lines = append(lines, catalog.say(key, "count", fmt.Sprint(blocked)))
	}
	if pdfUnreadable > 0 {
		lines = append(lines, catalog.say("pdf_unreadable", "count", fmt.Sprint(pdfUnreadable)))
	}
	if tooLarge > 0 {
		lines = append(lines, catalog.say("too_large", "count", fmt.Sprint(tooLarge), "limit", fmt.Sprint(chat.PDFLimitBytes(os.Getenv("DOKJA_PDF_MAX_MIB"))>>20)))
	}
	if other := len(data.Failures) - blocked - pdfUnreadable - tooLarge; other > 0 {
		lines = append(lines, catalog.say("failed", "count", fmt.Sprint(other)))
	}
	if data.Omitted > 0 {
		lines = append(lines, catalog.say("omitted", "count", fmt.Sprint(data.Omitted), "limit", fmt.Sprint(chat.MaxWebLinks)))
	}
	if len(data.ServedStored) > 0 {
		lines = append(lines, catalog.say("served_stored", "count", fmt.Sprint(len(data.ServedStored))))
	}
	if len(data.Deferred) > 0 {
		lines = append(lines, catalog.say("deferred_promise", "count", fmt.Sprint(len(data.Deferred))))
	}
	if data.FailureOnly {
		lines = append(lines, catalog.say("recovery"))
	}
	return strings.Join(lines, "\n")
}

func webAttachmentPrompt(event core.Event) string {
	locale, _ := event.Payload["locale"].(string)
	if strings.HasPrefix(locale, "en") {
		locale = "en"
	} else {
		locale = "pt"
	}
	return webReadingCatalogs[locale].say("document_prompt")
}

// A quotation attributed to a passage is checked against that passage; one with no citation
// is checked only from quoteMinWords words, since shorter ones are as often a gloss, a
// translation or a name as a quote. Case, punctuation and ellipses do not count, word order does.
func verifyWebQuotes(reply string, sources []knowledgeSource) string {
	for _, pattern := range []*regexp.Regexp{curlyQuote, angleQuote, straightQuote} {
		reply = pattern.ReplaceAllStringFunc(reply, func(match string) string {
			groups := pattern.FindStringSubmatch(match)
			cited := strings.TrimSpace(groups[2]) != ""
			if !cited && len(strings.Fields(groups[1])) < quoteMinWords {
				return match
			}
			pieces := quotePieces(groups[1])
			if len(pieces) == 0 {
				pieces = []string{normalizeQuote(groups[1])}
			}
			selected := sources
			if cited {
				selected = citedSources(groups[2], sources)
			}
			// The library verifier owns quotations explicitly attributed only to library passages.
			web, library := false, false
			for _, source := range selected {
				if source.Web {
					web = true
				} else {
					library = true
				}
			}
			if !web && library {
				return match
			}
			for _, source := range selected {
				if source.Web && pieces[0] != "" && containsPieces(source.Text, pieces) {
					return match
				}
			}
			return strings.Replace(match, groups[1], quoteRemoved, 1)
		})
	}
	return reply
}

// Both ordinary and archived turns share source numbering and untrusted evidence framing.
func (h *ChatDomainHandler) withWebSources(ctx context.Context, event core.Event, session, question string, sources []knowledgeSource) (core.Event, []knowledgeSource, *webTurnData) {
	web, data := h.webSources(ctx, event, session, question)
	if data == nil {
		return event, sources, nil
	}
	event.Context = cloneChatContext(event.Context)
	event.Context["web_reading_turn"] = data
	if len(web) == 0 && len(data.Failures) > 0 {
		data.FailureOnly = true
		return event, nil, data
	}
	for i := range sources {
		sources[i].Number += len(web)
	}
	return event, append(web, sources...), data
}

package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
	"sync"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// deferredReadingTTL bounds how long a promised follow-up may take. After it, the
// registry drops the entry silently: an ancient "I am still trying" is worse than none.
const deferredReadingTTL = 10 * time.Minute

// deferredReadBudget bounds one background re-read attempt: far above the turn's own
// budget, since nothing is waiting on it anymore.
const deferredReadBudget = 4 * time.Minute

// deferredFollowupBrief is the fixed system text of a deferred follow-up generation.
// The model improvises neither the promise nor the settlement; this turn only carries
// the fresh evidence and asks for the reply in the persona's voice.
func deferredFollowupBrief() string { return promptfiles.MustRead("deferred_followup_brief") }

// deferredReading is one promised follow-up. It names where it came from, never
// anything the fetch must re-derive: the reader works from the URL alone.
type deferredReading struct {
	ChannelID string     `json:"channel_id"`
	ReplyToID string     `json:"reply_to_id"`
	Question  string     `json:"question"`
	URL       string     `json:"url"`
	Event     core.Event `json:"event"`
	ExpiresAt time.Time  `json:"expires_at"`
}

// DeferredReadingStore persists the promise registry across restarts. The handler
// snapshots the whole registry on every mutation; a restart loads the snapshot,
// drops expired entries and re-arms the survivors.
type DeferredReadingStore interface {
	Save(ctx context.Context, data []byte) error
	Load(ctx context.Context) (data []byte, found bool, err error)
}

// deferredReadingRegistry holds the promises of one process and mirrors them to a
// store when one is wired. Entries themselves stay volatile in memory; the store is
// the recovery source (ADR 0017, resilient registry).
type deferredReadingRegistry struct {
	mu      sync.Mutex
	entries map[string]deferredReading // channelID + "\x00" + url
	now     func() time.Time
	store   DeferredReadingStore
}

func newDeferredReadingRegistry() *deferredReadingRegistry {
	return &deferredReadingRegistry{entries: map[string]deferredReading{}, now: time.Now}
}

// WithStore wires durable persistence; every mutation is mirrored.
func (r *deferredReadingRegistry) WithStore(store DeferredReadingStore) *deferredReadingRegistry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = store
	return r
}

func deferredReadingKey(channelID, url string) string { return channelID + "\x00" + url }

func (r *deferredReadingRegistry) persistLocked(ctx context.Context) {
	if r.store == nil {
		return
	}
	snapshot := make([]deferredReading, 0, len(r.entries))
	for _, entry := range r.entries {
		snapshot = append(snapshot, entry)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		logger.Info(fmt.Sprintf("deferred readings snapshot failed: %v", err))
		return
	}
	if len(snapshot) == 0 {
		encoded = nil
	}
	if err := r.store.Save(ctx, encoded); err != nil {
		logger.Info(fmt.Sprintf("deferred readings persistence failed: %v", err))
	}
}

// restore loads the persisted promises, keeps the unexpired ones and reports them
// so the handler can re-arm their background runs. A broken document forgets
// everything: promises are niceties, never load-bearing state.
func (r *deferredReadingRegistry) restore(ctx context.Context) []deferredReading {
	r.mu.Lock()
	store := r.store
	r.mu.Unlock()
	if store == nil {
		return nil
	}
	data, found, err := store.Load(ctx)
	if err != nil || !found {
		if err != nil {
			logger.Info(fmt.Sprintf("deferred readings restore failed: %v", err))
		}
		return nil
	}
	var snapshot []deferredReading
	if json.Unmarshal(data, &snapshot) != nil {
		logger.Info("deferred readings snapshot was unreadable; forgotten")
		return nil
	}
	now := r.now()
	kept := map[string]deferredReading{}
	var rearm []deferredReading
	for _, entry := range snapshot {
		if now.After(entry.ExpiresAt) {
			continue
		}
		kept[deferredReadingKey(entry.ChannelID, entry.URL)] = entry
		rearm = append(rearm, entry)
	}
	r.mu.Lock()
	r.entries = kept
	r.mu.Unlock()
	if len(rearm) > 0 {
		logger.Info(fmt.Sprintf("deferred readings restored count=%d", len(rearm)))
	}
	return rearm
}

// add records one promise. It refuses when the channel already holds one — expired
// entries are dropped first — or when the same URL is already pending: a channel
// must never accumulate promises.
func (r *deferredReadingRegistry) add(ctx context.Context, entry deferredReading) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for key, pending := range r.entries {
		if now.After(pending.ExpiresAt) {
			delete(r.entries, key)
			continue
		}
		if pending.ChannelID == entry.ChannelID || key == deferredReadingKey(entry.ChannelID, entry.URL) {
			return false
		}
	}
	entry.ExpiresAt = now.Add(deferredReadingTTL)
	r.entries[deferredReadingKey(entry.ChannelID, entry.URL)] = entry
	r.persistLocked(ctx)
	return true
}

// claim removes and returns one pending entry, or false when it expired or is gone.
func (r *deferredReadingRegistry) claim(ctx context.Context, channelID, url string) (deferredReading, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := deferredReadingKey(channelID, url)
	entry, ok := r.entries[key]
	if !ok {
		return deferredReading{}, false
	}
	delete(r.entries, key)
	r.persistLocked(ctx)
	if r.now().After(entry.ExpiresAt) {
		logger.Info(fmt.Sprintf("deferred reading expired channel=%s url=%s", channelID, url))
		return deferredReading{}, false
	}
	return entry, true
}

// ProactiveDeliverer posts a new message to a channel with no request driving it.
// Implemented by the Discord interface client; nil disables deferred readings.
type ProactiveDeliverer interface {
	DeliverReply(ctx context.Context, channelID, content, replyToMessageID string, embeds []clients.DiscordDeliveryEmbed) error
}

// WithProactiveDeliverer lets a failed transient reading defer: the handler re-reads
// in the background and delivers a cited follow-up reply to the original channel.
func (h *ChatDomainHandler) WithProactiveDeliverer(deliverer ProactiveDeliverer) *ChatDomainHandler {
	h.proactive = deliverer
	return h
}

// WithDeferredReadingStore makes the promise registry resilient: every mutation is
// mirrored, and a restart restores the pending promises and re-arms their runs.
func (h *ChatDomainHandler) WithDeferredReadingStore(store DeferredReadingStore) *ChatDomainHandler {
	h.deferred.WithStore(store)
	return h
}

// deferReading records one promise and starts its background re-read. The turn has
// already reported the failure; nothing here can change that reply. A refused or
// expired promise settles nowhere: the registry drops it and logs.
func (h *ChatDomainHandler) deferReading(parent context.Context, event core.Event, url, question string) {
	if h.proactive == nil || h.web == nil {
		return
	}
	h.restoreDeferredReadings(parent)
	entry := deferredReading{
		ChannelID: event.Channel.ID,
		ReplyToID: resultString(event.Context, "message_id"),
		Question:  question,
		URL:       url,
		Event:     event,
	}
	if !h.deferred.add(parent, entry) {
		return
	}
	go h.runDeferredReading(parent, entry)
}

// restoreDeferredReadings runs at most once per process: it re-arms the promises a
// previous process persisted, so a restart settles them instead of dropping them.
func (h *ChatDomainHandler) restoreDeferredReadings(ctx context.Context) {
	h.deferredRestoreOnce.Do(func() {
		for _, entry := range h.deferred.restore(ctx) {
			go h.runDeferredReading(context.Background(), entry)
		}
	})
}

// runDeferredReading settles one promise: re-read with the long budget, then either
// deliver the cited follow-up or one fixed notice that it failed for good. It never
// touches the originating turn, which ended long before this runs.
func (h *ChatDomainHandler) runDeferredReading(parent context.Context, entry deferredReading) {
	defer func() {
		if value := recover(); value != nil {
			configuration, ok := value.(*promptfiles.Error)
			if !ok {
				panic(value)
			}
			logger.Error("load deferred reading prompts", configuration)
			h.settleDeferredReading(context.WithoutCancel(parent), entry, nil)
		}
	}()

	settled := false
	defer func() {
		if _, ok := h.deferred.claim(context.Background(), entry.ChannelID, entry.URL); ok && !settled {
			logger.Info(fmt.Sprintf("deferred reading finished without a settlement channel=%s url=%s", entry.ChannelID, entry.URL))
		}
	}()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), deferredReadBudget)
	defer cancel()
	result, kind, err := readChatLink(ctx, h.web.domain, entry.URL)
	var document chat.WebDocument
	if err == nil {
		document, err = decodeWebDocument(result)
	}
	if err != nil {
		h.settleDeferredReading(ctx, entry, nil)
		return
	}
	if kind == "post" {
		h.appendPostMediaReadings(ctx, h.web.domain, entry.URL, &document)
	}
	h.learnFromReading(ctx, entry.Event, kind, map[string]any{"url": document.URL}, func(storeCtx context.Context) {
		h.knowledgePersist.persistWebReading(storeCtx, entry.Event, document)
	})
	h.upsertDeferredReading(entry, document)
	if err := promptfiles.Validate("deferred_followup_brief", "web_conversation_brief"); err != nil {
		logger.Error("load deferred reading prompts", err)
		h.settleDeferredReading(ctx, entry, nil)
		settled = true
		return
	}
	sources := webDocumentSources([]chat.WebDocument{document})
	messages := withWebEvidence([]map[string]string{{"role": "system", "content": deferredFollowupBrief()}}, sources, &webTurnData{})
	generated, err := generateConversationMessages(ctx, h.generator, messages)
	if err != nil {
		logger.Info(fmt.Sprintf("deferred follow-up generation failed channel=%s url=%s: %v", entry.ChannelID, entry.URL, err))
		h.settleDeferredReading(ctx, entry, nil)
		return
	}
	reply := verifyWebQuotes(chatReplyFromResult(generated), sources)
	h.settleDeferredReading(ctx, entry, []string{reply})
	settled = true
}

// upsertDeferredReading folds the fresh evidence into the session's kept reading set,
// under the same key a live turn would use, so a later follow-up question finds it.
func (h *ChatDomainHandler) upsertDeferredReading(entry deferredReading, document chat.WebDocument) {
	if h.web == nil {
		return
	}
	baseKey := chatSessionBaseKey(entry.Event)
	h.sessions.mu.Lock()
	sessionID := ""
	if state, ok := h.sessions.items[baseKey]; ok {
		sessionID = state.id
	}
	h.sessions.mu.Unlock()
	h.web.upsert(baseKey, sessionID, document)
}

// settleDeferredReading delivers the follow-up reply, or — when there is none — the
// fixed notice that the retry failed. Delivery failure is logged; the promise still
// counted as settled, since nothing else can consume it.
func (h *ChatDomainHandler) settleDeferredReading(ctx context.Context, entry deferredReading, replies []string) {
	content := ""
	if len(replies) > 0 {
		content = replies[0]
	}
	if content == "" {
		content = h.deferredNotice(entry.Event)
	}
	if content == "" {
		return
	}
	if err := h.proactive.DeliverReply(ctx, entry.ChannelID, content, entry.ReplyToID, nil); err != nil {
		logger.Info(fmt.Sprintf("deferred follow-up delivery failed channel=%s url=%s: %v", entry.ChannelID, entry.URL, err))
		return
	}
	if len(replies) > 0 {
		// The settlement is an ordinary assistant turn in the session transcript, so a
		// later conversation can refer to what came back.
		if err := h.sessions.CompleteTurn(ctx, entry.Event, replies[0]); err != nil {
			logger.Info(fmt.Sprintf("deferred follow-up transcript failed channel=%s: %v", entry.ChannelID, err))
		}
	}
}

// deferredNotice is the fixed settlement sentence, in the speaker's locale.
func (h *ChatDomainHandler) deferredNotice(event core.Event) string {
	locale, _ := event.Payload["locale"].(string)
	if !strings.HasPrefix(locale, "en") {
		locale = "pt"
	} else {
		locale = "en"
	}
	return webReadingCatalogs[locale].say("deferred_failed")
}

// webDocumentSources numbers one fresh document exactly like a live reading turn does.
func webDocumentSources(documents []chat.WebDocument) []knowledgeSource {
	var sources []knowledgeSource
	for _, document := range documents {
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
	return sources
}

// generateConversationMessages runs a message-list turn when the provider supports it
// and falls back to a single prompt, exactly like an ordinary chat turn.
func generateConversationMessages(ctx context.Context, generator ChatGenerator, messages []map[string]string) (map[string]any, error) {
	if conversation, ok := generator.(ChatConversationGenerator); ok {
		return conversation.GenerateMessages(ctx, messages)
	}
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message["role"]+": "+message["content"])
	}
	return generator.Generate(ctx, strings.Join(parts, "\n\n"))
}

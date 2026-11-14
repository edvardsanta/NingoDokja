package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	digest "read_books/dokja_domain/dokja_digest"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
)

const (
	// maxMemeImageBytes bounds the picture a proposal keeps, so the queue stays light.
	maxMemeImageBytes = 2 << 20
	maxMemeTextRunes  = 8000
	memeKindFile      = "file"
	memeKindLink      = "link"
	// maxMemeReadings bounds the readings of media that run at once, and memeReadingBudget one of them:
	// a post reader downloads for up to about a minute and a half before the text is read.
	maxMemeReadings = 2
	// maxMemeFileBytes is the largest file read for its text: the perception provider refuses a
	// download over twenty million bytes, so a bigger one is not worth carrying.
	maxMemeFileBytes  = 20_000_000
	memeReadingBudget = 4 * time.Minute
)

// memeReadingReason is the shape of a fixed reason a reading failed with.
var memeReadingReason = regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)

// memeAttachmentFetcher downloads a posted file through the meme service, which owns that access.
type memeAttachmentFetcher interface {
	FetchAttachment(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// memeDataTextReader reads the text of media the caller holds as bytes. A post reader's file lives on
// the host, behind a loopback address the perception provider in its container cannot open, so the
// bytes travel instead of the address. A reader that cannot do it leaves the address path as it was.
type memeDataTextReader interface {
	ReadTextData(ctx context.Context, data []byte, contentType string) (map[string]any, error)
}

// discordPostMedia says which meme Ningo posted as a Discord message, when the delivery was recorded.
type discordPostMedia interface {
	DiscordPostMedia(channelID, messageID string) (mediaURL string, found bool, err error)
}

// memeTagKindRules is what a meme-tag proposal supplies: a label for one meme, keyed by the same
// identity the stored example has, so what is proposed and what is stored address one item. The
// meme is kept the way it can be kept cheaply: a file as its address plus the text read from it, a
// small picture as base64, and a page that is not a file only as its link, resolved on approval.
func memeTagKindRules() knowledgedomain.ProposalKindRules {
	return knowledgedomain.ProposalKindRules{
		Kind: knowledgedomain.HashtagKnowledgeKind,
		Validate: func(payload map[string]any) (map[string]any, error) {
			ref := knowledgedomain.StableMediaRef(payloadString(payload, "url"))
			example, err := knowledgedomain.PrepareHashtagExample(ref, payloadString(payload, "tag"))
			if err != nil {
				return nil, err
			}
			if !isHTTPAddress(ref) {
				return nil, fmt.Errorf("a meme proposal needs the meme's http(s) address")
			}
			canonical := map[string]any{"url": example.SourceURL(), "tag": strings.TrimSpace(payloadString(payload, "tag"))}
			media, _ := payload["media"].(map[string]any)
			kept := map[string]any{"kind": memeKindFile}
			if payloadString(media, "kind") == memeKindLink {
				kept["kind"] = memeKindLink
			}
			if text := strings.TrimSpace(payloadString(media, "text")); text != "" {
				if len([]rune(text)) > maxMemeTextRunes {
					return nil, fmt.Errorf("the text read from the meme is too long")
				}
				kept["text"] = text
			}
			if encoded := strings.TrimSpace(payloadString(media, "image_b64")); encoded != "" {
				data, err := base64.StdEncoding.Strict().DecodeString(encoded)
				contentType := payloadString(media, "content_type")
				if err != nil || len(data) == 0 || len(data) > maxMemeImageBytes || !strings.HasPrefix(contentType, "image/") {
					return nil, fmt.Errorf("a meme picture must be a bounded base64 image")
				}
				kept["image_b64"], kept["content_type"] = encoded, contentType
			}
			// how the background reading of the media stands, so the owner can tell it from a silence
			if reading := payloadString(media, "reading"); reading == "pending" || reading == "done" || reading == "failed" {
				kept["reading"] = reading
				if reason := payloadString(media, "reason"); reading == "failed" && memeReadingReason.MatchString(reason) {
					kept["reason"] = reason
				}
			}
			if mime := strings.TrimSpace(payloadString(media, "mime")); mime != "" && len(mime) <= 100 {
				kept["mime"] = mime
			}
			if address := strings.TrimSpace(payloadString(media, "media_url")); address != "" {
				if !isHTTPAddress(address) || len(address) > 2048 {
					return nil, fmt.Errorf("the meme's file address must be an http(s) address")
				}
				kept["media_url"] = address
			}
			canonical["media"] = kept
			return canonical, nil
		},
		Identity: func(payload map[string]any) string {
			return knowledgedomain.HashtagKnowledgeID(payloadString(payload, "url"))
		},
	}
}

func isHTTPAddress(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

// approveMemeTag stores the proposed label as a Knowledge example carrying where it came from,
// through the same OCR-and-example path the owner's own tagging uses. A tag the owner edits while
// approving is still a group record: the owner's later retag is what makes it theirs.
func (h *KnowledgeProposalsHandler) approveMemeTag(ctx context.Context, proposal knowledgedomain.Proposal, edit map[string]any) (map[string]any, error) {
	for key := range edit {
		if key != "tag" && key != "text" {
			return map[string]any{"refused": "uneditable"}, nil
		}
	}
	ref := payloadString(proposal.Payload, "url")
	label := payloadString(proposal.Payload, "tag")
	if edited := strings.TrimSpace(payloadString(edit, "tag")); edited != "" {
		label = edited
	}
	example, err := knowledgedomain.PrepareHashtagExample(ref, label)
	if err != nil {
		return map[string]any{"refused": "invalid_tag"}, nil
	}
	media, _ := proposal.Payload["media"].(map[string]any)
	text := strings.TrimSpace(payloadString(edit, "text"))
	if text == "" {
		text = strings.TrimSpace(payloadString(media, "text"))
	}
	if text == "" {
		var refused string
		if text, refused = h.readMemeText(ctx, ref, media); refused != "" {
			return map[string]any{"refused": refused}, nil
		}
	}
	current := h.currentStoredRecord(ctx, knowledgedomain.HashtagKnowledgeKind, knowledgedomain.HashtagKnowledgeID(ref))
	if _, err := h.knowledge.StoreHashtagExampleWithProvenance(ctx, example, text, provenanceFor(proposal)); err != nil {
		return nil, err
	}
	outcome := map[string]any{"stored": 1}
	if origin, found := current["origin"]; found {
		outcome["replaced_origin"] = origin
	}
	return outcome, nil
}

// readMemeText reads the text of a meme that has none kept. A page that is not a file is read through
// the post readers, and its media is read from the bytes they hand over. A refusal keeps the proposal
// pending with a fixed reason and never shows the reader's own words: "media_unreachable" says the
// meme could not be fetched, which is not the same as "no_text", the meme having none to read.
func (h *KnowledgeProposalsHandler) readMemeText(ctx context.Context, ref string, media map[string]any) (string, string) {
	if h.memeText == nil {
		return "", "no_text"
	}
	source := ref
	if payloadString(media, "kind") == memeKindLink {
		if reader, ok := h.memeText.(memeDataTextReader); ok {
			return h.readPostText(ctx, ref, reader)
		}
		resolved, refused := h.resolvePost(ctx, ref)
		if refused != "" {
			return "", refused
		}
		source = resolved
	} else if address := payloadString(media, "media_url"); address != "" {
		source = address
	}
	answer, err := h.memeText.ReadText(ctx, source)
	return memeTextFrom(answer, err)
}

// memeTextFrom turns what a text read answered into the text or a fixed reason.
func memeTextFrom(answer map[string]any, err error) (string, string) {
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no text") {
			return "", "no_text"
		}
		return "", "media_unreachable"
	}
	text := strings.TrimSpace(stringValue(answer, "text"))
	if text == "" {
		return "", "no_text"
	}
	return text, ""
}

// readPostText asks the post readers for the link's media with its bytes and reads the text of the
// first file that comes whole. A file too large to travel inline is not read.
func (h *KnowledgeProposalsHandler) readPostText(ctx context.Context, link string, reader memeDataTextReader) (string, string) {
	answer, err := digest.New(h.web).Handle(ctx, digest.Request{Action: digest.ActionReadLinkDelivery, Event: digest.Event{Payload: map[string]any{"url": link}}})
	if err != nil {
		return "", "failed"
	}
	if reason := resultString(answer, "refused"); reason != "" {
		return "", reason
	}
	files, _ := answer["media"].([]digest.DeliveryMedia)
	tooLarge := false
	for _, file := range files {
		data, contentType := []byte(nil), file.ContentType
		switch {
		case file.DataB64 != "":
			decoded, err := base64.StdEncoding.Strict().DecodeString(file.DataB64)
			if err != nil {
				continue
			}
			data = decoded
		case file.MediaToken != "":
			// Feeds holds the file and hands it over in pieces, whatever its size
			if file.Size > maxMemeFileBytes {
				tooLarge = true
				continue
			}
			held, err := h.mediaInPieces(ctx, file.MediaToken, file.Size)
			if err != nil {
				return "", "media_unreachable"
			}
			data = held
		}
		if len(data) == 0 {
			continue
		}
		return memeTextFrom(reader.ReadTextData(ctx, data, contentType))
	}
	if tooLarge {
		return "", "media_too_large"
	}
	return "", "no_media"
}

// mediaInPieces reads a file Feeds holds, piece by piece, up to the size it declared.
func (h *KnowledgeProposalsHandler) mediaInPieces(ctx context.Context, token string, size int64) ([]byte, error) {
	reader := digest.New(h.web)
	held := make([]byte, 0, size)
	for int64(len(held)) < size {
		piece, err := reader.MediaChunk(ctx, token, int64(len(held)))
		if err != nil {
			return nil, err
		}
		if piece.Size != size || int64(len(held))+int64(len(piece.Data)) > maxMemeFileBytes {
			return nil, fmt.Errorf("media piece does not fit the file")
		}
		held = append(held, piece.Data...)
	}
	return held, nil
}

// resolvePost asks the post readers for the media of a link and returns the first picture or video.
func (h *KnowledgeProposalsHandler) resolvePost(ctx context.Context, link string) (string, string) {
	answer, err := digest.New(h.web).Handle(ctx, digest.Request{Action: digest.ActionReadLink, Event: digest.Event{Payload: map[string]any{"url": link}}})
	if err != nil {
		return "", "failed"
	}
	if reason := resultString(answer, "refused"); reason != "" {
		return "", reason
	}
	post, _ := answer["post"].(map[string]any)
	items, _ := post["media"].([]map[string]any)
	if items == nil {
		raw, _ := post["media"].([]any)
		for _, item := range raw {
			if entry, ok := item.(map[string]any); ok {
				items = append(items, entry)
			}
		}
	}
	for _, item := range items {
		if address := payloadString(item, "url"); address != "" {
			return address, ""
		}
	}
	return "", "no_media"
}

func (h *KnowledgeProposalsHandler) currentMemeTag(ctx context.Context, payload map[string]any) (map[string]any, bool) {
	current := h.currentStoredRecord(ctx, knowledgedomain.HashtagKnowledgeKind, knowledgedomain.HashtagKnowledgeID(payloadString(payload, "url")))
	return current, current != nil
}

// WithMemeTags supplies what a meme-tag proposal needs besides the shared handler: the text
// reader, the file fetcher, the record of what Ningo posted, and the people Ningo knows. Without
// them a proposal is refused rather than guessed.
func (h *KnowledgeProposalsHandler) WithMemeTags(text MemeTextReader, fetch memeAttachmentFetcher, posts discordPostMedia, people []chat.KnownPerson) *KnowledgeProposalsHandler {
	h.memeText, h.memeFetch, h.memePosts, h.people = text, fetch, posts, people
	return h
}

// WithBackground says how the slow reading of a meme's media is run. It is a goroutine unless a test
// wants to run it itself.
func (h *KnowledgeProposalsHandler) WithBackground(run func(func())) *KnowledgeProposalsHandler {
	h.background = run
	return h
}

// knownPerson matches the sender against the people Ningo knows, the same mapping chat uses.
func (h *KnowledgeProposalsHandler) knownPerson(event core.Event) (chat.KnownPerson, bool) {
	id := strings.TrimSpace(event.User.ID)
	if id == "" {
		return chat.KnownPerson{}, false
	}
	for _, person := range h.people {
		if person.Discord != "" && person.Discord == id {
			return person, true
		}
	}
	return chat.KnownPerson{}, false
}

func personLabel(person chat.KnownPerson, id string) string {
	name := person.Name
	if person.RealName != "" {
		name = person.RealName
	}
	if id != "" {
		return fmt.Sprintf("%s <%s>", name, id)
	}
	return name
}

// ProposeMemeTag reads a tag posted in a meme channel. It answers nothing, in every case, and it is
// quick: it holds the proposal and leaves the reading of the meme's media, which downloads and reads
// text for as long as a minute or two, to the background. A known person's tag waits there for the
// owner to decide in the desktop, with the text already read; anyone else's is kept nowhere. The
// owner's own tag needs no approval: once its text is read it is stored as theirs, the way their
// tagging in the desktop is, and it stays in the queue only when no text can be read, so they can
// give it there. The channel never learns what became of it.
func (h *KnowledgeProposalsHandler) ProposeMemeTag(ctx context.Context, event core.Event) (map[string]any, error) {
	silent := map[string]any{"reply": "", "meme_tag": "ignored"}
	if h == nil || h.store == nil {
		return silent, nil
	}
	person, known := h.knownPerson(event)
	owner, _ := event.Context["is_owner"].(bool)
	if owner && !known {
		// The owner is not in the people mapping; the proposal names them as they appear.
		person, known = chat.KnownPerson{Name: strings.TrimSpace(event.User.Name)}, true
	}
	if !known {
		return silent, nil
	}
	content, _ := event.Context["content"].(string)
	attachments, _ := event.Payload["media_attachments"].([]chat.WebAttachment)
	replyTo, isReply := event.Context["reply_to"].(map[string]any)
	post, ok := knowledgedomain.ParseMemeTagPost(content, len(attachments) > 0, isReply)
	if !ok {
		return silent, nil
	}
	proposer := personLabel(person, event.User.ID)
	meme := h.memeFrom(event, post, attachments, replyTo)
	if meme.ref == "" {
		return silent, nil
	}
	sentBy, messageID := proposer, resultString(event.Context, "message_id")
	if post.Form == knowledgedomain.MemeTagReply {
		sentBy, messageID = personLabel(chat.KnownPerson{Name: resultString(replyTo, "author_name")}, resultString(replyTo, "author_id")), resultString(replyTo, "message_id")
	}
	source := knowledgedomain.ProposalSource{ChannelID: event.Channel.ID, MessageID: messageID}
	if guild := resultString(event.Context, "guild_id"); guild != "" && source.ChannelID != "" && messageID != "" {
		source.Permalink = fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guild, source.ChannelID, messageID)
	}
	id, duplicate, err := h.Propose(ctx, knowledgedomain.HashtagKnowledgeKind, map[string]any{"url": meme.ref, "tag": post.Hashtag, "media": meme.media}, source, proposer, sentBy)
	if err != nil {
		logger.Info(fmt.Sprintf("meme tag proposal was not held: %v", err))
		return silent, nil
	}
	logger.Info(fmt.Sprintf("meme tag proposal held id=%s duplicate=%t", id, duplicate))
	if !duplicate {
		h.background(func() { h.readMemeProposal(id, owner) })
	}
	return map[string]any{"reply": "", "meme_tag": "held"}, nil
}

// readMemeProposal reads what is kept of a held meme proposal's media, in the background: the text,
// and a small picture. It writes them into the proposal while it is still pending, so the owner's
// approval finds them ready, and it stores the owner's own tag once its text is there. A proposal
// decided meanwhile is left alone, and so is one the reading cannot complete: it stays pending for the
// owner, who can give the text in the desktop.
func (h *KnowledgeProposalsHandler) readMemeProposal(id string, owner bool) {
	h.memeWork <- struct{}{}
	defer func() { <-h.memeWork }()
	ctx, cancel := context.WithTimeout(context.Background(), memeReadingBudget)
	defer cancel()
	row, found, err := h.store.GetProposal(id)
	if err != nil || !found {
		return
	}
	proposal, err := proposalFromRow(row)
	if err != nil {
		return
	}
	ref := payloadString(proposal.Payload, "url")
	media, _ := proposal.Payload["media"].(map[string]any)
	kept := map[string]any{}
	for key, value := range media {
		kept[key] = value
	}
	if payloadString(kept, "text") == "" {
		if text, refused := h.readMemeText(ctx, ref, kept); refused == "" {
			kept["text"], kept["reading"] = text, "done"
			delete(kept, "reason")
		} else {
			kept["reading"], kept["reason"] = "failed", refused
			logger.Info(fmt.Sprintf("meme proposal %s: its text could not be read in the background: %s", id, refused))
		}
	} else {
		kept["reading"] = "done"
	}
	if payloadString(kept, "image_b64") == "" && payloadString(kept, "kind") == memeKindFile && h.memeFetch != nil && strings.HasPrefix(payloadString(kept, "mime"), "image/") {
		if data, fetchedType, err := h.memeFetch.FetchAttachment(ctx, payloadString(kept, "media_url")); err == nil && len(data) > 0 && len(data) <= maxMemeImageBytes && strings.HasPrefix(fetchedType, "image/") {
			kept["image_b64"], kept["content_type"] = base64.StdEncoding.EncodeToString(data), fetchedType
		}
	}
	proposal.Payload["media"] = kept
	encoded, err := json.Marshal(proposal.Payload)
	if err != nil {
		return
	}
	if updated, err := h.store.UpdateProposalPayload(id, string(encoded)); err != nil || !updated {
		return
	}
	if owner && payloadString(kept, "text") != "" && h.storeOwnersTag(ctx, payloadString(proposal.Payload, "tag"), memeProposal{ref: ref, media: kept}) {
		if err := h.store.DeleteProposal(id); err != nil {
			logger.Info(fmt.Sprintf("owner meme tag %s was stored but its proposal was not removed: %v", id, err))
		}
	}
}

// storeOwnersTag stores the owner's own tag as a Knowledge example of theirs, through the same path
// their tagging in the desktop uses, and says whether it did. It does not when the text of the meme
// cannot be read, and the tag is then held so the owner can supply it.
func (h *KnowledgeProposalsHandler) storeOwnersTag(ctx context.Context, tag string, meme memeProposal) bool {
	example, err := knowledgedomain.PrepareHashtagExample(meme.ref, tag)
	if err != nil {
		return false
	}
	text := strings.TrimSpace(payloadString(meme.media, "text"))
	if text == "" {
		var refused string
		if text, refused = h.readMemeText(ctx, meme.ref, meme.media); refused != "" {
			logger.Info(fmt.Sprintf("owner meme tag is held for the text: %s", refused))
			return false
		}
	}
	if _, err := h.knowledge.StoreHashtagExampleWithProvenance(ctx, example, text, knowledgedomain.Provenance{Origin: knowledgedomain.OriginOwner}); err != nil {
		logger.Info(fmt.Sprintf("owner meme tag was not stored, held instead: %v", err))
		return false
	}
	logger.Info("owner meme tag stored")
	return true
}

// memeProposal is the meme a post names: the address that identifies it, and what the message says of it.
type memeProposal struct {
	ref   string
	media map[string]any
}

// memeFrom finds the meme a tag post is about, from what the message carries and nothing slow: nothing
// is downloaded or read here. A reply to a post Ningo made names the meme it was recorded under, so
// the tag lands on the same item the stored examples and citations use.
func (h *KnowledgeProposalsHandler) memeFrom(event core.Event, post knowledgedomain.MemeTagPost, attachments []chat.WebAttachment, replyTo map[string]any) memeProposal {
	if post.Form == knowledgedomain.MemeTagReply {
		if h.memePosts != nil {
			if recorded, found, err := h.memePosts.DiscordPostMedia(event.Channel.ID, resultString(replyTo, "message_id")); err == nil && found {
				return memeProposal{ref: recorded, media: map[string]any{"kind": memeKindFile, "media_url": recorded, "reading": "pending"}}
			}
		}
		attachments, _ = replyTo["media_attachments"].([]chat.WebAttachment)
		if len(attachments) == 0 {
			if link := knowledgedomain.FirstLink(resultString(replyTo, "content")); link != "" {
				return linkMeme(link)
			}
			return memeProposal{}
		}
	}
	for _, attachment := range attachments {
		if strings.HasPrefix(attachment.ContentType, "image/") || strings.HasPrefix(attachment.ContentType, "video/") {
			return memeProposal{ref: knowledgedomain.StableMediaRef(attachment.URL), media: map[string]any{
				"kind": memeKindFile, "media_url": attachment.URL, "mime": attachment.ContentType, "reading": "pending",
			}}
		}
	}
	if post.Link != "" {
		return linkMeme(post.Link)
	}
	return memeProposal{}
}

// linkMeme keeps a link: a file as it is read, a page only as its link for the post reader to resolve.
func linkMeme(link string) memeProposal {
	if knowledgedomain.LinkIsMediaFile(link) {
		return memeProposal{ref: knowledgedomain.StableMediaRef(link), media: map[string]any{"kind": memeKindFile, "media_url": link, "reading": "pending"}}
	}
	return memeProposal{ref: knowledgedomain.StableMediaRef(link), media: map[string]any{"kind": memeKindLink, "reading": "pending"}}
}

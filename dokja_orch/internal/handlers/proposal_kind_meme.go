package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

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
)

// memeAttachmentFetcher downloads a posted file through the meme service, which owns that access.
type memeAttachmentFetcher interface {
	FetchAttachment(ctx context.Context, url string) (data []byte, contentType string, err error)
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

// readMemeText reads the text of a meme that has none kept. A page that is not a file is resolved
// through the post readers first, and its first picture or video is read; a refusal keeps the
// proposal pending with a fixed reason and never shows the reader's own words.
func (h *KnowledgeProposalsHandler) readMemeText(ctx context.Context, ref string, media map[string]any) (string, string) {
	if h.memeText == nil {
		return "", "no_text"
	}
	source := ref
	if payloadString(media, "kind") == memeKindLink {
		resolved, refused := h.resolvePost(ctx, ref)
		if refused != "" {
			return "", refused
		}
		source = resolved
	} else if address := payloadString(media, "media_url"); address != "" {
		source = address
	}
	answer, err := h.memeText.ReadText(ctx, source)
	if err != nil {
		return "", "no_text"
	}
	text := strings.TrimSpace(stringValue(answer, "text"))
	if text == "" {
		return "", "no_text"
	}
	return text, ""
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

// ProposeMemeTag reads a tag proposed in a meme channel and holds it for the owner. It answers
// nothing, in every case: a known person's proposal is held, anyone else's is kept nowhere, and the
// owner has the desktop for their own tags. The channel never learns what became of it.
func (h *KnowledgeProposalsHandler) ProposeMemeTag(ctx context.Context, event core.Event) (map[string]any, error) {
	silent := map[string]any{"reply": "", "meme_tag": "ignored"}
	if h == nil || h.store == nil {
		return silent, nil
	}
	if owner, _ := event.Context["is_owner"].(bool); owner {
		return silent, nil
	}
	person, known := h.knownPerson(event)
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
	meme := h.memeFrom(ctx, event, post, attachments, replyTo)
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
	return map[string]any{"reply": "", "meme_tag": "held"}, nil
}

// memeProposal is the meme a post names: the address that identifies it, and what is kept of it.
type memeProposal struct {
	ref   string
	media map[string]any
}

// memeFrom finds the meme a tag post is about and keeps what it can of it cheaply. A reply to a post
// Ningo made names the meme it was recorded under, so the tag lands on the same item the stored
// examples and citations use.
func (h *KnowledgeProposalsHandler) memeFrom(ctx context.Context, event core.Event, post knowledgedomain.MemeTagPost, attachments []chat.WebAttachment, replyTo map[string]any) memeProposal {
	if post.Form == knowledgedomain.MemeTagReply {
		if h.memePosts != nil {
			if recorded, found, err := h.memePosts.DiscordPostMedia(event.Channel.ID, resultString(replyTo, "message_id")); err == nil && found {
				return h.fileMeme(ctx, recorded, recorded, "")
			}
		}
		attachments, _ = replyTo["media_attachments"].([]chat.WebAttachment)
		if len(attachments) == 0 {
			if link := knowledgedomain.FirstLink(resultString(replyTo, "content")); link != "" {
				return h.linkMeme(ctx, link)
			}
			return memeProposal{}
		}
	}
	for _, attachment := range attachments {
		if strings.HasPrefix(attachment.ContentType, "image/") || strings.HasPrefix(attachment.ContentType, "video/") {
			return h.fileMeme(ctx, knowledgedomain.StableMediaRef(attachment.URL), attachment.URL, attachment.ContentType)
		}
	}
	if post.Link != "" {
		return h.linkMeme(ctx, post.Link)
	}
	return memeProposal{}
}

// linkMeme keeps a link: a file as it is read, a page only as its link for the post reader to
// resolve when the owner approves it.
func (h *KnowledgeProposalsHandler) linkMeme(ctx context.Context, link string) memeProposal {
	if knowledgedomain.LinkIsMediaFile(link) {
		return h.fileMeme(ctx, knowledgedomain.StableMediaRef(link), link, "")
	}
	return memeProposal{ref: knowledgedomain.StableMediaRef(link), media: map[string]any{"kind": memeKindLink}}
}

// fileMeme keeps a file's address and the text read from it while its link still works. A small
// picture is kept as base64, so the queue can still show it once the link has expired.
func (h *KnowledgeProposalsHandler) fileMeme(ctx context.Context, ref, address, contentType string) memeProposal {
	media := map[string]any{"kind": memeKindFile, "media_url": address}
	if h.memeText != nil {
		if answer, err := h.memeText.ReadText(ctx, address); err == nil {
			if text := strings.TrimSpace(stringValue(answer, "text")); text != "" && len([]rune(text)) <= maxMemeTextRunes {
				media["text"] = text
			}
		}
	}
	if h.memeFetch != nil && (contentType == "" || strings.HasPrefix(contentType, "image/")) {
		if data, fetchedType, err := h.memeFetch.FetchAttachment(ctx, address); err == nil && len(data) > 0 && len(data) <= maxMemeImageBytes && strings.HasPrefix(fetchedType, "image/") {
			media["image_b64"], media["content_type"] = base64.StdEncoding.EncodeToString(data), fetchedType
		}
	}
	return memeProposal{ref: ref, media: media}
}

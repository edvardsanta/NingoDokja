package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/dokja_store"
	"read_books/internal/core"
)

// memeTextStub reads text by address and remembers what it was asked.
type memeTextStub struct {
	text  map[string]string
	asked []string
}

func (s *memeTextStub) ReadText(_ context.Context, url string) (map[string]any, error) {
	s.asked = append(s.asked, url)
	return map[string]any{"text": s.text[url]}, nil
}

type memeFetchStub struct {
	data        []byte
	contentType string
}

func (s memeFetchStub) FetchAttachment(context.Context, string) ([]byte, string, error) {
	return s.data, s.contentType, nil
}

type memePostsStub map[string]string

func (s memePostsStub) DiscordPostMedia(channelID, messageID string) (string, bool, error) {
	media, ok := s[channelID+"/"+messageID]
	return media, ok, nil
}

// postReaderStub answers the Feeds calls a post reader needs: which readers exist and what one reads.
type postReaderStub struct {
	webReadingService
	post    map[string]any
	refused string
}

func (s *postReaderStub) Dispatch(ctx context.Context, kind string, payload map[string]any) (map[string]any, error) {
	switch kind {
	case "feeds.status":
		return map[string]any{"providers": []any{map[string]any{"id": "reader", "enabled": true, "provides": []any{"social.resolve.v1"}}}}, nil
	case "feeds.invoke":
		if s.refused != "" {
			return map[string]any{"handled": true, "refused": s.refused}, nil
		}
		return map[string]any{"handled": true, "post": s.post}, nil
	}
	return s.webReadingService.Dispatch(ctx, kind, payload)
}

const (
	signedClip = "https://cdn.example/attachments/chan-1/msg-1/clip.mp4?ex=aa&is=bb&hm=cc"
	stableClip = "https://cdn.example/attachments/chan-1/msg-1/clip.mp4"
)

var friends = []chat.KnownPerson{{Name: "Sam", RealName: "Samuel", Discord: "user-sam"}}

type memeHarness struct {
	h         *KnowledgeProposalsHandler
	knowledge *proposalsKnowledgeStub
	text      *memeTextStub
	db        *store.DB
}

func newMemeHarness(t *testing.T, web *postReaderStub, fetch memeFetchStub, posts memePostsStub) memeHarness {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dokja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	knowledge := &proposalsKnowledgeStub{}
	text := &memeTextStub{text: map[string]string{signedClip: "GAME OVER", "https://example.com/clip.mp4": "FROM A LINK", "https://memes.example/a.mp4": "THE ORIGINAL"}}
	h := NewKnowledgeProposalsHandler(db, knowledge, web, &fakeReader{}).WithMemeTags(text, fetch, posts, friends)
	return memeHarness{h: h, knowledge: knowledge, text: text, db: db}
}

func tagPost(user, content string, media []chat.WebAttachment, replyTo map[string]any) core.Event {
	event := core.Event{Source: core.SourceDiscord, Type: "message.created", User: core.EventUser{ID: user}, Channel: core.EventChannel{ID: "chan-1"},
		Payload: map[string]any{}, Context: map[string]any{"content": content, "message_id": "msg-1", "guild_id": "guild-1", "meme_tag_post": true}}
	if media != nil {
		event.Payload["media_attachments"] = media
	}
	if replyTo != nil {
		event.Context["reply_to"] = replyTo
	}
	return event
}

func (m memeHarness) pending(t *testing.T) []map[string]any {
	t.Helper()
	queued, err := m.h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if err != nil {
		t.Fatal(err)
	}
	return queued["proposals"].([]map[string]any)
}

func propose(t *testing.T, m memeHarness, event core.Event) map[string]any {
	t.Helper()
	answer, err := m.h.Handle(context.Background(), event, coreStep("propose-meme-tag"))
	if err != nil {
		t.Fatal(err)
	}
	if answer["reply"] != "" {
		t.Fatalf("a proposal must never be answered: %v", answer)
	}
	return answer
}

var video = []chat.WebAttachment{{URL: signedClip, Filename: "clip.mp4", ContentType: "video/mp4", Size: 10}}

func TestAKnownPersonsTagPostIsHeldWithWhoWhereAndWhatWasRead(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	if answer := propose(t, m, tagPost("user-sam", "#SomeGames", video, nil)); answer["meme_tag"] != "held" {
		t.Fatal(answer)
	}
	held := m.pending(t)
	if len(held) != 1 {
		t.Fatalf("one proposal: %v", held)
	}
	got := held[0]
	payload := got["payload"].(map[string]any)
	media := payload["media"].(map[string]any)
	if payload["url"] != stableClip || payload["tag"] != "#SomeGames" || media["kind"] != "file" || media["text"] != "GAME OVER" || media["media_url"] != signedClip {
		t.Fatalf("the proposal keeps the stable address, the tag and what was read: %v", payload)
	}
	source := got["source"].(map[string]any)
	if got["kind"] != "meme_hashtag" || got["proposed_by"] != "Samuel <user-sam>" || got["sent_by"] != "Samuel <user-sam>" ||
		source["channel_id"] != "chan-1" || source["permalink"] != "https://discord.com/channels/guild-1/chan-1/msg-1" {
		t.Fatalf("who and where: %v", got)
	}
	if len(m.knowledge.payloads) != 0 {
		t.Fatal("a pending proposal must not reach Knowledge")
	}
}

func TestASmallPictureIsKeptAsBase64AndAVideoIsNot(t *testing.T) {
	picture := []byte("\x89PNG small")
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{data: picture, contentType: "image/png"}, nil)
	image := []chat.WebAttachment{{URL: signedClip, ContentType: "image/png"}}
	propose(t, m, tagPost("user-sam", "#SomeGames", image, nil))
	media := m.pending(t)[0]["payload"].(map[string]any)["media"].(map[string]any)
	if media["image_b64"] != base64.StdEncoding.EncodeToString(picture) || media["content_type"] != "image/png" {
		t.Fatalf("the picture survives the link expiring: %v", media)
	}
	other := newMemeHarness(t, &postReaderStub{}, memeFetchStub{data: picture, contentType: "image/png"}, nil)
	propose(t, other, tagPost("user-sam", "#SomeGames", video, nil))
	if _, kept := other.pending(t)[0]["payload"].(map[string]any)["media"].(map[string]any)["image_b64"]; kept {
		t.Fatal("a video keeps its link, not a copy")
	}
}

func TestOnlyAKnownPersonsOrTheOwnersPostIsHeld(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	for name, event := range map[string]core.Event{
		"a stranger": tagPost("user-unknown", "#SomeGames", video, nil),
		"no tag":     tagPost("user-sam", "nice one", video, nil),
		"no meme":    tagPost("user-sam", "#SomeGames", nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			if answer := propose(t, m, event); answer["meme_tag"] != "ignored" {
				t.Fatal(answer)
			}
		})
	}
	if len(m.pending(t)) != 0 {
		t.Fatal("nothing may be held")
	}
}

func TestTheOwnersOwnTagIsStoredAtOnceAsTheirsWithNoApproval(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	event := tagPost("user-owner", "#SomeGames", video, nil)
	event.User.Name = "Eduardo"
	event.Context["is_owner"] = true
	if answer := propose(t, m, event); answer["meme_tag"] != "stored" {
		t.Fatalf("the owner does not approve their own tag: %v", answer)
	}
	if len(m.pending(t)) != 0 {
		t.Fatal("nothing is left in the queue for the owner to approve")
	}
	stored := m.knowledge.payloads[0]
	if stored["source_id"] != knowledgedomain.HashtagKnowledgeID(stableClip) || stored["title"] != "#SomeGames" || stored["body"] != "GAME OVER" {
		t.Fatalf("it is the same example the desktop's tagging stores: %v", stored)
	}
	provenance := stored["metadata"].(map[string]any)["provenance"].(knowledgedomain.Provenance)
	if provenance.Origin != knowledgedomain.OriginOwner || provenance.ProposedBy != "" {
		t.Fatalf("it is the owner's own record: %+v", provenance)
	}
}

func TestTheOwnersTagIsHeldOnlyWhenNoTextCanBeReadFromTheMeme(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	m.text.text[signedClip] = ""
	event := tagPost("user-owner", "#SomeGames", video, nil)
	event.User.Name = "Eduardo"
	event.Context["is_owner"] = true
	if answer := propose(t, m, event); answer["meme_tag"] != "held" {
		t.Fatalf("without text the owner is asked for it in the desktop: %v", answer)
	}
	held := m.pending(t)
	if len(held) != 1 || held[0]["proposed_by"] != "Eduardo <user-owner>" || len(m.knowledge.payloads) != 0 {
		t.Fatalf("%v", held)
	}
}

func TestAReplyToOneOfNingosPostsNamesTheMemeItWasRecordedUnder(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, memePostsStub{"chan-1/msg-0": "https://memes.example/a.mp4"})
	reply := map[string]any{"message_id": "msg-0", "author_id": "webhook-1", "author_name": "Ningo Dokja", "content": "#Old"}
	propose(t, m, tagPost("user-sam", "#IagoGames", nil, reply))
	got := m.pending(t)[0]
	payload := got["payload"].(map[string]any)
	if payload["url"] != "https://memes.example/a.mp4" || payload["tag"] != "#IagoGames" {
		t.Fatalf("the tag lands on the recorded meme, not on a copy of it: %v", payload)
	}
	if payload["media"].(map[string]any)["text"] != "THE ORIGINAL" {
		t.Fatalf("the text is read from the original: %v", payload["media"])
	}
	source := got["source"].(map[string]any)
	if got["proposed_by"] != "Samuel <user-sam>" || got["sent_by"] != "Ningo Dokja <webhook-1>" ||
		source["message_id"] != "msg-0" || source["permalink"] != "https://discord.com/channels/guild-1/chan-1/msg-0" {
		t.Fatalf("the tag author and the meme's sender are two things, and the source is the meme: %v", got)
	}
}

func TestAReplyToAMemberPostUsesItsAttachmentOrLink(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	propose(t, m, tagPost("user-sam", "#SomeGames", nil, map[string]any{"message_id": "msg-0", "author_id": "user-2", "author_name": "Alex", "media_attachments": video}))
	if payload := m.pending(t)[0]["payload"].(map[string]any); payload["url"] != stableClip || m.pending(t)[0]["sent_by"] != "Alex <user-2>" {
		t.Fatal(m.pending(t))
	}
	linked := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	propose(t, linked, tagPost("user-sam", "#SomeGames", nil, map[string]any{"message_id": "msg-0", "author_id": "user-2", "author_name": "Alex", "content": "look https://example.com/clip.mp4"}))
	if payload := linked.pending(t)[0]["payload"].(map[string]any); payload["url"] != "https://example.com/clip.mp4" {
		t.Fatal(payload)
	}
	nothing := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	if answer := propose(t, nothing, tagPost("user-sam", "#SomeGames", nil, map[string]any{"message_id": "msg-0", "author_id": "user-2", "content": "just words"})); answer["meme_tag"] != "ignored" {
		t.Fatal("a reply to a message with no meme proposes nothing")
	}
}

func TestAPageLinkIsKeptAsItsLinkAndResolvedWhenTheOwnerApproves(t *testing.T) {
	page := "https://example.com/shorts/abc"
	web := &postReaderStub{post: map[string]any{"url": page, "media": []any{map[string]any{"kind": "video", "url": "https://example.com/resolved.mp4"}}}}
	m := newMemeHarness(t, web, memeFetchStub{}, nil)
	m.text.text["https://example.com/resolved.mp4"] = "FROM THE POST"
	propose(t, m, tagPost("user-sam", page+" #IagoGames", nil, nil))
	held := m.pending(t)
	if media := held[0]["payload"].(map[string]any)["media"].(map[string]any); media["kind"] != "link" || media["text"] != nil || len(m.text.asked) != 0 {
		t.Fatalf("a page is kept as its link and nothing is read yet: %v asked=%v", media, m.text.asked)
	}
	approved, err := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": held[0]["id"]}), coreStep("approve-knowledge-proposal"))
	if err != nil || approved["approved"] != true {
		t.Fatalf("%v %v", approved, err)
	}
	if got := m.knowledge.payloads[0]["body"]; got != "FROM THE POST" {
		t.Fatalf("the text comes from the media the post reader found: %v", got)
	}
}

func TestAPostReaderRefusalKeepsTheProposalPendingWithAFixedReason(t *testing.T) {
	web := &postReaderStub{refused: "login_required"}
	m := newMemeHarness(t, web, memeFetchStub{}, nil)
	propose(t, m, tagPost("user-sam", "https://example.com/shorts/abc #IagoGames", nil, nil))
	held := m.pending(t)
	answer, err := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": held[0]["id"]}), coreStep("approve-knowledge-proposal"))
	if err != nil || answer["approved"] != false || answer["reason"] != "login_required" || len(m.pending(t)) != 1 {
		t.Fatalf("%v %v", answer, err)
	}
}

func TestApprovingAMemeTagStoresTheLabelWithItsOriginAndTheChannel(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	propose(t, m, tagPost("user-sam", "#SomeGames", video, nil))
	id := m.pending(t)[0]["id"]
	approved, err := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id, "edit": map[string]any{"tag": "#BetterGames"}}), coreStep("approve-knowledge-proposal"))
	if err != nil || approved["approved"] != true {
		t.Fatalf("%v %v", approved, err)
	}
	stored := m.knowledge.payloads[0]
	if stored["source_id"] != knowledgedomain.HashtagKnowledgeID(stableClip) || stored["kind"] != "meme_hashtag" || stored["title"] != "#BetterGames" || stored["body"] != "GAME OVER" || stored["source_ref"] != stableClip {
		t.Fatalf("the example uses the same identity, kind and text path as the owner's tagging: %v", stored)
	}
	provenance := stored["metadata"].(map[string]any)["provenance"].(knowledgedomain.Provenance)
	if provenance.Origin != knowledgedomain.OriginGroup || provenance.ProposedBy != "Samuel <user-sam>" || provenance.Channel != "chan-1" ||
		provenance.Permalink != "https://discord.com/channels/guild-1/chan-1/msg-1" {
		t.Fatalf("an edited tag is still a group record, with the channel it came from: %+v", provenance)
	}
	if len(m.pending(t)) != 0 {
		t.Fatal("an approved proposal is gone")
	}
}

func TestEditingAMemeTagProposalOnlyChangesTheTagOrTheText(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	propose(t, m, tagPost("user-sam", "#SomeGames", video, nil))
	id := m.pending(t)[0]["id"]
	answer, _ := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id, "edit": map[string]any{"url": "https://example.com/other.mp4"}}), coreStep("approve-knowledge-proposal"))
	if answer["reason"] != "uneditable" {
		t.Fatalf("the meme's identity is not editable: %v", answer)
	}
	answer, _ = m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id, "edit": map[string]any{"tag": "not a tag"}}), coreStep("approve-knowledge-proposal"))
	if answer["reason"] != "invalid_tag" || len(m.pending(t)) != 1 {
		t.Fatalf("an invalid tag leaves it pending: %v", answer)
	}
	answer, _ = m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id, "edit": map[string]any{"text": "the owner's own words"}}), coreStep("approve-knowledge-proposal"))
	if answer["approved"] != true || m.knowledge.payloads[0]["body"] != "the owner's own words" {
		t.Fatalf("the owner can supply the text: %v", answer)
	}
}

func TestAnApprovalWithNoTextToReadIsRefusedUntilTheOwnerGivesIt(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	m.text.text[signedClip] = ""
	propose(t, m, tagPost("user-sam", "#SomeGames", video, nil))
	id := m.pending(t)[0]["id"]
	answer, _ := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if answer["approved"] != false || answer["reason"] != "no_text" || len(m.pending(t)) != 1 {
		t.Fatalf("%v", answer)
	}
}

func TestTheQueueShowsTheLabelAMemeAlreadyHasAndAnApprovalSaysWhatItReplaces(t *testing.T) {
	m := newMemeHarness(t, &postReaderStub{}, memeFetchStub{}, nil)
	m.knowledge.documents = []any{map[string]any{"source_id": knowledgedomain.HashtagKnowledgeID(stableClip), "title": "#OwnersTag",
		"metadata": map[string]any{"provenance": map[string]any{"origin": "owner"}}}}
	propose(t, m, tagPost("user-sam", "#SomeGames", video, nil))
	held := m.pending(t)
	current := held[0]["current"].(map[string]any)
	if current["origin"] != "owner" || current["title"] != "#OwnersTag" {
		t.Fatalf("the owner decides with their own label next to the proposal: %v", current)
	}
	approved, _ := m.h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": held[0]["id"]}), coreStep("approve-knowledge-proposal"))
	if approved["replaced_origin"] != "owner" {
		t.Fatalf("an approval over the owner's label is their explicit decision and says so: %v", approved)
	}
}

func TestAMemeTagProposalBoundsItsPayload(t *testing.T) {
	_, err := knowledgedomain.ValidateProposal("meme_hashtag", map[string]any{"url": stableClip, "tag": "#SomeGames",
		"media": map[string]any{"image_b64": base64.StdEncoding.EncodeToString(make([]byte, maxMemeImageBytes+1)), "content_type": "image/png"}}, "sam", "sam")
	if err == nil || !strings.Contains(err.Error(), "bounded base64 image") {
		t.Fatalf("an oversized picture is refused: %v", err)
	}
	for name, payload := range map[string]map[string]any{
		"no address":  {"tag": "#SomeGames"},
		"a non-http":  {"url": "file:///tmp/a.mp4", "tag": "#SomeGames"},
		"a bad tag":   {"url": stableClip, "tag": "no spaces allowed"},
		"a long text": {"url": stableClip, "tag": "#SomeGames", "media": map[string]any{"text": strings.Repeat("x", maxMemeTextRunes+1)}},
	} {
		if _, err := knowledgedomain.ValidateProposal("meme_hashtag", payload, "sam", "sam"); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	_ = fmt.Sprint
}

package conversation

import (
	"fmt"
	"testing"
	"time"
)

func observation(id, speaker, text string) RealtimeMessage {
	return RealtimeMessage{MessageID: id, SessionID: "session", SpeakerID: speaker, Text: text, Status: "final", Revision: 1, TimestampStart: time.Unix(100, 0), TimestampEnd: time.Unix(102, 0)}
}
func started(t *testing.T) *ConversationContextEngine {
	t.Helper()
	e := NewConversationContextEngine()
	if err := e.Start("session", "guild", "voice", "text", "a"); err != nil {
		t.Fatal(err)
	}
	return e
}
func TestRealtimeConsentScopeAndNoImplicitTopicAssignment(t *testing.T) {
	e := started(t)
	m := observation("one", "b", "Technical question")
	if e.Observe("guild", "text", m) == nil {
		t.Fatal("nonconsenting speaker accepted")
	}
	if e.Consent("session", "other", "text", "b", true) == nil {
		t.Fatal("foreign guild consent accepted")
	}
	if err := e.Consent("session", "guild", "text", "b", true); err != nil {
		t.Fatal(err)
	}
	m.ThreadIDs = []string{"untrusted-thread"}
	m.ReplyTo = "untrusted-message"
	if e.Observe("guild", "other", m) == nil {
		t.Fatal("foreign channel accepted")
	}
	if err := e.Observe("guild", "text", m); err != nil {
		t.Fatal(err)
	}
	got := e.Snapshot()[0].Messages[0]
	if len(got.ThreadIDs) != 0 || got.ReplyTo != "" {
		t.Fatal("interface invented semantic facts")
	}
	if err := e.Consent("session", "guild", "text", "b", false); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot()[0].Messages) != 0 || e.Observe("guild", "text", m) == nil {
		t.Fatal("withdrawal did not erase/revoke")
	}
}
func TestRealtimeRevisionsAreIdempotentAndEvidenceIsNotDuplicated(t *testing.T) {
	e := started(t)
	m := observation("one", "a", "JWT question")
	if err := e.Observe("guild", "text", m); err != nil {
		t.Fatal(err)
	}
	if err := e.Observe("guild", "text", m); err != nil {
		t.Fatal(err)
	}
	m.Text = "corrected question"
	if e.Observe("guild", "text", m) == nil {
		t.Fatal("conflicting same revision accepted")
	}
	m.Revision = 2
	if err := e.Observe("guild", "text", m); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot()[0].Messages) != 1 || e.Snapshot()[0].Messages[0].Text != m.Text {
		t.Fatal("revision duplicated")
	}
	m.Status = "partial"
	m.Revision = 3
	if e.Observe("guild", "text", m) == nil {
		t.Fatal("partial persisted as final")
	}
}
func TestRealtimeOverlapBoundsExpiryStopAndSnapshotIsolation(t *testing.T) {
	e := started(t)
	for i := 0; i < 520; i++ {
		m := observation(fmt.Sprint(i), "a", "same overlapping interval")
		m.CaptureSequence = i
		if err := e.Observe("guild", "text", m); err != nil {
			t.Fatal(err)
		}
	}
	snap := e.Snapshot()
	if len(snap[0].Messages) != 512 {
		t.Fatal("journal not bounded")
	}
	if !snap[0].Messages[0].TimestampStart.Equal(snap[0].Messages[1].TimestampStart) {
		t.Fatal("overlap rewritten")
	}
	snap[0].Messages[0].Text = "mutation"
	snap[0].Consenting[0] = "mutation"
	if e.Snapshot()[0].Messages[0].Text == "mutation" || e.Snapshot()[0].Consenting[0] == "mutation" {
		t.Fatal("mutable snapshot escaped")
	}
	if e.Stop("session", "foreign", "text", "a") == nil {
		t.Fatal("foreign stop accepted")
	}
	if err := e.Stop("session", "guild", "text", "a"); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot()) != 0 {
		t.Fatal("stop retained data")
	}
	e = started(t)
	future := time.Now().Add(16 * time.Minute)
	e.now = func() time.Time { return future }
	if len(e.Snapshot()) != 0 {
		t.Fatal("expired session retained")
	}
}

func TestRealtimeCaptureOrderSurvivesOutOfOrderRecognition(t *testing.T) {
	e := started(t)
	later := observation("later", "a", "answer")
	later.CaptureSequence = 2
	earlier := observation("earlier", "a", "question")
	earlier.CaptureSequence = 1
	if err := e.Observe("guild", "text", later); err != nil {
		t.Fatal(err)
	}
	if err := e.Observe("guild", "text", earlier); err != nil {
		t.Fatal(err)
	}
	messages := e.Snapshot()[0].Messages
	if messages[0].MessageID != "earlier" || messages[1].MessageID != "later" {
		t.Fatal("recognition order became conversation order")
	}
}

func TestChannelObserverAdmitsSpeakersWithoutJoiningAndRetainsScope(t *testing.T) {
	e := NewConversationContextEngine()
	if err := e.StartChannel("session", "guild", "voice", "text", "a"); err != nil {
		t.Fatal(err)
	}
	m := observation("one", "b", "second speaker")
	if e.Observe("other", "text", m) == nil {
		t.Fatal("foreign guild accepted")
	}
	if e.Observe("guild", "other", m) == nil {
		t.Fatal("foreign origin accepted")
	}
	if err := e.Observe("guild", "text", m); err != nil {
		t.Fatal(err)
	}
	snap := e.Snapshot()
	if len(snap[0].Messages) != 1 || len(snap[0].Consenting) != 2 {
		t.Fatal("channel speaker missing")
	}
	if err := e.Stop("session", "guild", "text", "b"); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot()) != 0 || e.Observe("guild", "text", m) == nil {
		t.Fatal("stopped observer admitted evidence")
	}
}

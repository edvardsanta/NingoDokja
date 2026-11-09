package chat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSpeakableReplyRemovesMarkupAndKeepsShortReplies(t *testing.T) {
	got, err := SpeakableReply("**Malthus** wrote [this](https://example.com/essay).")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got, "*[]()") || strings.Contains(got, "example.com") {
		t.Fatalf("markup or link left in speech: %q", got)
	}
	if !strings.Contains(got, "Malthus") {
		t.Fatalf("words lost: %q", got)
	}
}

func TestSpeakableReplyEndsLongRepliesAtASentence(t *testing.T) {
	sentence := "Population grows faster than food in his argument. "
	got, err := SpeakableReply(strings.Repeat(sentence, 40))
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(got); n == 0 || n > MaxSpokenReply {
		t.Fatalf("spoken length %d outside 1..%d", n, MaxSpokenReply)
	}
	if !strings.HasSuffix(got, ".") {
		t.Fatalf("speech does not end at a sentence: %q", got[len(got)-20:])
	}
}

func TestSpeakableReplyStopsAtAWordWithoutSentenceEnds(t *testing.T) {
	got, err := SpeakableReply(strings.Repeat("palavra ", 200))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "palavra…") || utf8.RuneCountInString(got) > MaxSpokenReply+1 {
		t.Fatalf("expected a word-bounded cut, got %d runes ending %q", utf8.RuneCountInString(got), got[len(got)-12:])
	}
}

func TestSpeakableReplyRejectsOversizeAndSilence(t *testing.T) {
	if _, err := SpeakableReply(strings.Repeat("a", MaxSpeakReplyInput+1)); err == nil {
		t.Fatal("oversize reply accepted")
	}
	got, err := SpeakableReply("[SILENT]")
	if err != nil || got != "" {
		t.Fatalf("silent reply should say nothing, got %q, %v", got, err)
	}
}

func TestSpeakableReplyReadsListsAndCitationsAsSentences(t *testing.T) {
	got, err := SpeakableReply("## Malthus\n\n- **População** cresce\n- comida não\n\nVeja [o ensaio](https://example.com) [1].\n\n```go\nx := 1\n```\nFim.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Malthus. População cresce. comida não. Veja o ensaio. Fim."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

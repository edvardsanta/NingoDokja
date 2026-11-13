package chat

import "testing"

func TestMacroRecallFollowsTheLiveContext(t *testing.T) {
	live := []string{
		"What is the best storage for application sessions?",
		"Sessions are short lived, so a local SQLite file is enough for the live conversation history.",
	}
	cases := []struct {
		name     string
		question string
		live     []string
		use      bool
		reason   string
	}{
		{"empty", "   ", live, false, RecallEmptyQuestion},
		{"asks for the past in Portuguese", "Lembra o que a gente decidiu sobre o cache?", live, true, RecallAsksForPast},
		{"asks for the past in English", "Remember what we discussed about backups", live, true, RecallAsksForPast},
		{"a cue wins over a covered topic", "last time we covered sessions and storage history", live, true, RecallAsksForPast},
		{"a new session has only the macro context", "Which database holds the durable ideas about storage?", nil, true, RecallNoLiveContext},
		{"a blank live context counts as none", "Which database holds the durable ideas?", []string{" ", "\n"}, true, RecallNoLiveContext},
		{"a short follow-up leans on the live context", "e o backup?", live, false, RecallShortFollowUp},
		{"a follow-up with a pronoun stays live", "why is that enough?", live, false, RecallShortFollowUp},
		{"the live context already covers the topic", "Is SQLite enough for the conversation history of sessions?", live, false, RecallLiveCovers},
		{"a new topic reaches beyond the live context", "How should the macro context vectors be indexed in PostgreSQL?", live, true, RecallBeyondLive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecideMacroRecall(c.question, c.live)
			if got.Use != c.use || got.Reason != c.reason {
				t.Fatalf("%q -> %+v, want use=%t reason=%s", c.question, got, c.use, c.reason)
			}
		})
	}
}

func TestMacroRecallIsDeterministicAndNeverEmptyHanded(t *testing.T) {
	live := []string{"We talked about indexing vectors in PostgreSQL for the macro context."}
	first := DecideMacroRecall("Should vectors for the macro context be indexed in PostgreSQL?", live)
	for i := 0; i < 20; i++ {
		if again := DecideMacroRecall("Should vectors for the macro context be indexed in PostgreSQL?", live); again != first {
			t.Fatal("the same turn decided differently", first, again)
		}
	}
	for _, question := range []string{"", "?", "ok", "Lembra?", "a very long question about something entirely unrelated to the history"} {
		if got := DecideMacroRecall(question, live); got.Reason == "" {
			t.Fatalf("%q decided without a reason", question)
		}
	}
}

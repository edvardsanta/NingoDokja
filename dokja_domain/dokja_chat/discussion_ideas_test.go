package chat

import (
	"strings"
	"testing"
)

func TestIdeasRequireVerbatimEvidenceAndBoundedIndexes(t *testing.T) {
	evidence := []IdeaEvidence{{Text: "Persist events before processing them to recover after failures."}}
	good := `{"ideas":[{"text":"The document proposes persisting events before processing.","quote":"Persist events before processing","evidence":0}]}`
	ideas, err := ParseDiscussionIdeas(good, evidence)
	if err != nil || len(ideas) != 1 {
		t.Fatal(ideas, err)
	}
	for _, raw := range []string{
		`{"ideas":[{"text":"A claim","quote":"Unsupported quotation","evidence":0}]}`,
		`{"ideas":[{"text":"A claim","quote":"Persist events before processing","evidence":1}]}`,
		`{"ideas":[{"text":"","quote":"Persist events before processing","evidence":0}]}`,
		`not json`,
	} {
		if _, err := ParseDiscussionIdeas(raw, evidence); err == nil {
			t.Fatal("invalid extraction accepted", raw)
		}
	}
	if ideas, err := ParseDiscussionIdeas(`{"ideas":[]}`, evidence); err != nil || len(ideas) != 0 {
		t.Fatal(ideas, err)
	}
}

func TestMacroContextScopeSeparatesPrivateAndPublicContext(t *testing.T) {
	if MacroContextScope(true, false, "desktop:one") != "owner:private" || MacroContextScope(true, false, "phone:two") != "owner:private" {
		t.Fatal("owner context did not span interfaces")
	}
	if MacroContextScope(true, true, "guild:channel") != "guild:channel" {
		t.Fatal("owner private context leaked into public channel")
	}
	if MacroContextScope(false, false, "person:one") != "person:one" || MacroContextScope(false, false, "") != "" {
		t.Fatal("invalid non-owner scope")
	}
}

func TestAParticipantIsNamedByTheKnownNameWithTheRealNameBesideIt(t *testing.T) {
	cases := []struct {
		who  ExtractionParticipant
		want string
	}{
		{ExtractionParticipant{Name: "handle", KnownAs: "Nick", RealName: "Alex"}, "Nick (Alex)"},
		{ExtractionParticipant{Name: "handle", KnownAs: "Sam"}, "Sam"},
		{ExtractionParticipant{Name: "handle", KnownAs: "Sam", RealName: "Sam"}, "Sam"},
		{ExtractionParticipant{Name: "handle"}, "handle"},
		{ExtractionParticipant{RealName: "Alex"}, ""},
	}
	for _, c := range cases {
		if got := c.who.Label(); got != c.want {
			t.Fatalf("%+v -> %q, want %q", c.who, got, c.want)
		}
	}
}

func TestAnIdeaKeepsASpeakerOnlyWhenItIsAGivenParticipant(t *testing.T) {
	evidence := []IdeaEvidence{{Text: "We will keep the profiles in PostgreSQL from now on."}}
	people := []ExtractionParticipant{{Name: "handle", KnownAs: "Nick", RealName: "Alex"}}
	ideas, err := ParseDiscussionIdeasFor(`{"ideas":[
		{"kind":"decision","text":"Nick (Alex) decided to keep profiles in PostgreSQL.","speaker":"Nick (Alex)","quote":"keep the profiles in PostgreSQL","evidence":0},
		{"kind":"preference","text":"Someone prefers it.","speaker":"Somebody Invented","quote":"keep the profiles in PostgreSQL","evidence":0},
		{"kind":"correction","text":"No one in particular.","quote":"keep the profiles in PostgreSQL","evidence":0}]}`, evidence, people)
	if err != nil || len(ideas) != 3 {
		t.Fatal(ideas, err)
	}
	if ideas[0].Speaker != "Nick (Alex)" || ideas[1].Speaker != "" || ideas[2].Speaker != "" {
		t.Fatal("an invented speaker was trusted", ideas)
	}
	if ideas[1].Kind != "preference" || ideas[2].Kind != "correction" {
		t.Fatal("the new kinds were refused", ideas)
	}
	// Without participants nothing keeps a speaker, and the old entry point still works.
	plain, err := ParseDiscussionIdeas(`{"ideas":[{"kind":"decision","text":"x","speaker":"Nick (Alex)","quote":"keep the profiles in PostgreSQL","evidence":0}]}`, evidence)
	if err != nil || plain[0].Speaker != "" {
		t.Fatal(plain, err)
	}
	if _, err := ParseDiscussionIdeasFor(`{"ideas":[{"kind":"joke","text":"x","quote":"keep the profiles in PostgreSQL","evidence":0}]}`, evidence, people); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}

func TestTheExtractionPromptAsksForLittleAndForNamesNotAccounts(t *testing.T) {
	for _, want := range []string{"prefer none", "jokes, banter, teasing", "decision", "preference", "correction", "never as \"the user\"", "Never write account ids"} {
		if !strings.Contains(DiscussionIdeasPrompt, want) {
			t.Fatalf("the prompt lost %q", want)
		}
	}
}

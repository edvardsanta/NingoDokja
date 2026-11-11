package chat

import "testing"

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

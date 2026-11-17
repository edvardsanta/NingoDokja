package chat

import (
	"encoding/json"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
)

// DiscussionIdea is an attributed extraction, never a verified fact or instruction.
type DiscussionIdea struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Speaker is who the idea is about, as the application labelled them (ExtractionParticipant.Label).
	// It is kept only when it names one of the people the extraction was given.
	Speaker  string `json:"speaker,omitempty"`
	Quote    string `json:"quote"`
	Evidence int    `json:"evidence"`
}

type IdeaEvidence struct {
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`
	Label string `json:"label,omitempty"`
}

// ExtractionParticipant is someone speaking in the discussion, as the application knows them: the
// display name they chose, and, when the owner listed them, the name Ningo calls them by and their
// real name. Account ids never appear here.
type ExtractionParticipant struct {
	Name     string `json:"name,omitempty"`
	KnownAs  string `json:"known_as,omitempty"`
	RealName string `json:"real_name,omitempty"`
}

// Label is how an idea names the person: the known name with the real name beside it when they
// differ ("Nick (Alex)"), else the known name, else the display name, else empty.
func (p ExtractionParticipant) Label() string {
	switch {
	case p.KnownAs != "" && p.RealName != "" && p.RealName != p.KnownAs:
		return p.KnownAs + " (" + p.RealName + ")"
	case p.KnownAs != "":
		return p.KnownAs
	default:
		return p.Name
	}
}

// ParseDiscussionIdeas accepts only bounded ideas with verbatim supporting evidence. Without
// participants no idea keeps a speaker.
func ParseDiscussionIdeas(raw string, evidence []IdeaEvidence) ([]DiscussionIdea, error) {
	return ParseDiscussionIdeasFor(raw, evidence, nil)
}

// ParseDiscussionIdeasFor also keeps the speaker of an idea when it is exactly one of the
// participants' labels; any other value is dropped, never trusted, so a model cannot invent whom an
// idea is about.
func ParseDiscussionIdeasFor(raw string, evidence []IdeaEvidence, participants []ExtractionParticipant) ([]DiscussionIdea, error) {
	if len(raw) > 12000 {
		return nil, fmt.Errorf("idea extraction exceeds bounds")
	}
	var response struct {
		Ideas []DiscussionIdea `json:"ideas"`
	}
	if json.Unmarshal([]byte(raw), &response) != nil || len(response.Ideas) > 3 {
		return nil, fmt.Errorf("invalid idea extraction")
	}
	labels := map[string]bool{}
	for _, participant := range participants {
		if label := participant.Label(); label != "" {
			labels[label] = true
		}
	}
	for index := range response.Ideas {
		idea := &response.Ideas[index]
		if idea.Kind == "" {
			idea.Kind = "idea"
		}
		switch idea.Kind {
		case "idea", "decision", "question", "summary", "preference", "correction":
		default:
			return nil, fmt.Errorf("invalid context category")
		}
		if strings.TrimSpace(idea.Text) == "" || len(idea.Text) > 2000 || len(idea.Quote) < 12 || len(idea.Quote) > 1000 || idea.Evidence < 0 || idea.Evidence >= len(evidence) || !strings.Contains(evidence[idea.Evidence].Text, idea.Quote) {
			return nil, fmt.Errorf("unsupported discussion idea")
		}
		if !labels[idea.Speaker] {
			idea.Speaker = ""
		}
	}
	return response.Ideas, nil
}

// DiscussionIdeasPrompt asks for little, on purpose. Most of a chat is not worth keeping, and what is
// kept is read back into later answers, so a joke or a one-off dispute kept as an "idea" only adds noise.
func DiscussionIdeasPrompt() string { return promptfiles.MustRead("chat_discussion_ideas_prompt") }

// MacroContextScope shares context across verified private owner interfaces only.
func MacroContextScope(owner, shared bool, conversation string) string {
	if conversation == "" {
		return ""
	}
	if owner && !shared {
		return "owner:private"
	}
	return conversation
}

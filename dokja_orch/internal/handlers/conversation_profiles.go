package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/logger"
)

// ProfileSessionReader reads the stored chat sessions, where the exchanges are.
type ProfileSessionReader interface {
	All(ctx context.Context) ([]chat.KeyedSession, error)
}

// ProfileBook keeps the inferred profiles and which sessions belong to a recognized person.
type ProfileBook interface {
	conversation.ProfileStore
	SpeakerSessions
	PersonSessions(ctx context.Context) (map[string]string, error)
}

// profileSubject is someone a profile can be written about: the owner or a person the owner listed.
type profileSubject struct {
	key      string
	name     string
	realName string
	owner    bool
	discord  string
}

type profileSource struct {
	subjects  []profileSubject
	sessions  ProfileSessionReader
	book      ProfileBook
	generator taskJSONGenerator
	now       func() time.Time
}

// WithProfiles lets the owner's interfaces see how Ningo deals with each person: the owner and the
// people in the private configuration. Exchanges are found in the stored sessions by verified
// account, never by a name someone typed. The profile text is inferred by a model on request, from
// those exchanges alone, and nothing in a conversation reads it back. ownerDiscord is the owner's
// Discord account id, empty when none is configured.
func (h *ConversationDomainHandler) WithProfiles(ownerName, ownerDiscord string, people []chat.KnownPerson, sessions ProfileSessionReader, book ProfileBook, generator taskJSONGenerator) *ConversationDomainHandler {
	if sessions == nil || book == nil {
		return h
	}
	subjects := []profileSubject{{key: conversation.OwnerPerson, name: ownerName, owner: true, discord: ownerDiscord}}
	seen := map[string]bool{}
	for _, person := range people {
		key := conversation.PersonKey(person.Name)
		if person.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		subjects = append(subjects, profileSubject{key: key, name: person.Name, realName: person.RealName, discord: person.Discord})
	}
	h.profiles = &profileSource{subjects: subjects, sessions: sessions, book: book, generator: generator, now: func() time.Time { return time.Now().UTC() }}
	return h
}

// exchanges gathers, per subject, what they said and what Ningo answered, oldest session first.
//
// A Discord channel or voice room is shared: each message carries the alias of its account, and a
// subject's alias is derived from their configured account id. A session of one of the owner's own
// interfaces is the owner's. Any other session (a WhatsApp chat) belongs to someone only when the
// chat handler noted it while recognizing them.
func (p *profileSource) exchanges(ctx context.Context) (map[string][]conversation.Exchange, error) {
	sessions, err := p.sessions.All(ctx)
	if err != nil {
		return nil, err
	}
	noted, err := p.book.PersonSessions(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, subject := range p.subjects {
		known[subject.key] = true
	}
	found := map[string][]conversation.Exchange{}
	for _, session := range sessions {
		source, _, _ := strings.Cut(session.Key, ":")
		voice := source == "discord-voice"
		shared := voice || strings.HasPrefix(session.Key, "discord:channel:")
		aliases := map[string]string{}
		if shared {
			for _, subject := range p.subjects {
				if subject.discord != "" {
					aliases[chat.ParticipantAlias(session.Key, subject.discord)] = subject.key
				}
			}
		}
		whole := ""
		if _, owners := conversation.ScopeFor(source); owners && !shared {
			whole = conversation.OwnerPerson
		} else if person := noted[session.Key]; !shared && known[person] {
			whole = person
		}
		for i, message := range session.Messages {
			if message.Role != "user" {
				continue
			}
			reply := ""
			if i+1 < len(session.Messages) && session.Messages[i+1].Role == "assistant" {
				reply = session.Messages[i+1].Content
			}
			if whole != "" {
				found[whole] = append(found[whole], conversation.Exchange{Said: message.Content, Reply: reply})
				continue
			}
			segments := chat.ParticipantSegments(message.Content, voice)
			for index, segment := range segments {
				subject, ok := aliases[segment.Alias]
				if !ok {
					continue
				}
				exchange := conversation.Exchange{Voice: voice, Said: segment.Text}
				// A reply answers the last speaker of its turn.
				if index == len(segments)-1 {
					exchange.Reply = reply
				}
				found[subject] = append(found[subject], exchange)
			}
		}
	}
	return found, nil
}

func (h *ConversationDomainHandler) listProfiles(ctx context.Context) (map[string]any, error) {
	p := h.profiles
	if p == nil {
		return nil, conversation.ErrUnavailable
	}
	exchanges, err := p.exchanges(ctx)
	if err != nil {
		logger.Error("read exchanges for profiles", err)
		return nil, conversation.ErrUnavailable
	}
	kept, err := p.book.AllProfiles(ctx)
	if err != nil {
		logger.Error("read profiles", err)
		return nil, conversation.ErrUnavailable
	}
	byPerson := map[string]conversation.Profile{}
	for _, profile := range kept {
		byPerson[profile.Person] = profile
	}
	views := make([]map[string]any, 0, len(p.subjects))
	for _, subject := range p.subjects {
		views = append(views, profileView(subject, exchanges[subject.key], byPerson))
	}
	return map[string]any{"profiles": views, "min_exchanges": conversation.MinProfileExchanges, "inference": p.generator != nil}, nil
}

// generateProfile infers one person's profile again and keeps it in place of the last.
func (h *ConversationDomainHandler) generateProfile(ctx context.Context, person string) (map[string]any, error) {
	p := h.profiles
	if p == nil || p.generator == nil {
		return nil, conversation.ErrUnavailable
	}
	var subject *profileSubject
	for i := range p.subjects {
		if p.subjects[i].key == person {
			subject = &p.subjects[i]
		}
	}
	if subject == nil {
		return nil, conversation.ErrNotFound
	}
	exchanges, err := p.exchanges(ctx)
	if err != nil {
		logger.Error("read exchanges for a profile", err)
		return nil, conversation.ErrUnavailable
	}
	messages, used := conversation.ProfileMessages(subject.name, subject.realName, exchanges[subject.key])
	if used < conversation.MinProfileExchanges {
		return nil, conversation.ErrInvalid
	}
	result, err := p.generator.GenerateTaskJSON(ctx, "analysis", conversation.ProfileSchema(), messages, func(reply map[string]any) error {
		text, _ := reply["reply"].(string)
		_, err := conversation.ParseProfile(text)
		return err
	})
	if err != nil {
		// The cause names providers and attempts; the owner's interface only needs to know it failed.
		logger.Error("infer a profile", err)
		return nil, fmt.Errorf("the profile could not be inferred: no model gave a usable answer")
	}
	text, _ := result["reply"].(string)
	profile, err := conversation.ParseProfile(text)
	if err != nil {
		return nil, fmt.Errorf("the profile could not be inferred: no model gave a usable answer")
	}
	profile.Person, profile.Exchanges, profile.GeneratedAt = subject.key, used, p.now()
	profile.Provider, profile.Model = answeringModel(result["attempts"])
	if err := p.book.SaveProfile(ctx, profile); err != nil {
		logger.Error("keep a profile", err)
		return nil, conversation.ErrUnavailable
	}
	return map[string]any{"profile": profileView(*subject, exchanges[subject.key], map[string]conversation.Profile{subject.key: profile})}, nil
}

// answeringModel reads which provider and model answered from the task's attempts, without tying
// this handler to the client's types.
func answeringModel(attempts any) (string, string) {
	raw, err := json.Marshal(attempts)
	if err != nil {
		return "", ""
	}
	var list []struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Outcome  string `json:"outcome"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return "", ""
	}
	for _, attempt := range list {
		if attempt.Outcome == "success" {
			return attempt.Provider, attempt.Model
		}
	}
	return "", ""
}

func profileView(subject profileSubject, exchanges []conversation.Exchange, kept map[string]conversation.Profile) map[string]any {
	stats := conversation.StatsOf(exchanges)
	voice := 0
	for _, exchange := range exchanges {
		if exchange.Voice {
			voice++
		}
	}
	view := map[string]any{
		"person": subject.key, "name": subject.name, "real_name": subject.realName, "owner": subject.owner,
		"turns": stats.Turns, "voice_turns": voice, "answered": stats.Answered, "median_reply": stats.MedianReplyRunes,
	}
	if profile, ok := kept[subject.key]; ok {
		view["profile"] = map[string]any{
			"summary": profile.Summary, "tone": profile.Tone, "topics": profile.Topics, "patterns": profile.Patterns,
			"exchanges": profile.Exchanges, "generated_at": profile.GeneratedAt.Format(time.RFC3339Nano),
			"provider": profile.Provider, "model": profile.Model,
		}
	}
	return view
}

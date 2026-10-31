package handlers

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// A known person asks Ningo to keep something by saying so, and only then: nothing here is inferred
// and the model decides nothing. The phrases are few and tight, since a false match would keep what
// nobody asked to keep. The answer is in the language of the phrase used.

//go:embed locales/facts_*.json
var factLocales embed.FS

type factCatalog map[string]string

var factCatalogs = func() map[string]factCatalog {
	catalogs := map[string]factCatalog{}
	for _, locale := range []string{"pt", "en"} {
		raw, err := factLocales.ReadFile("locales/facts_" + locale + ".json")
		if err != nil {
			panic(err)
		}
		catalog := factCatalog{}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			panic(fmt.Sprintf("facts catalog %s: %v", locale, err))
		}
		catalogs[locale] = catalog
	}
	return catalogs
}()

func (c factCatalog) say(key string, values ...string) string {
	text := c[key]
	for i := 0; i+1 < len(values); i += 2 {
		text = strings.ReplaceAll(text, "{"+values[i]+"}", values[i+1])
	}
	return text
}

type factCommand int

const (
	factRemember factCommand = iota
	factList
	factForget
)

type factPhrase struct {
	command factCommand
	locale  string
	pattern *regexp.Regexp
}

// factNameCall is Ningo's name opening a message. The WhatsApp interface takes it off in a group, but
// the owner's own chats keep it, and "Ningo, guarde isso" means the same there.
var factNameCall = regexp.MustCompile(`(?i)^\s*(?:/ningo\b|ningo\s*[,:])\s*`)

const factSeparator = `[\s:,.\-–—]*`

var factPhrases = []factPhrase{
	{factRemember, "pt", regexp.MustCompile(`(?is)^(?:guarde|guarda|anote|anota|salve|salva)\s+(?:isso|isto)\b(?:\s+(?:para|pra)\s+mim)?` + factSeparator + `(.*)$`)},
	{factRemember, "pt", regexp.MustCompile(`(?is)^(?:guarde|guarda|anote|anota|salve|salva)(?:\s*:|\s+[\-–—])\s*(.+)$`)},
	{factRemember, "pt", regexp.MustCompile(`(?is)^(?:guarde|guarda|anote|anota|salve|salva)\s+que\s+(.+)$`)},
	{factRemember, "pt", regexp.MustCompile(`(?is)^(?:lembre|lembra)(?:-se)?\s+disso\b` + factSeparator + `(.*)$`)},
	{factRemember, "en", regexp.MustCompile(`(?is)^(?:remember|keep|save|note)\s+this\b` + factSeparator + `(.*)$`)},
	{factRemember, "en", regexp.MustCompile(`(?is)^(?:remember|save|note)\s*:\s*(.+)$`)},
	{factRemember, "en", regexp.MustCompile(`(?is)^remember\s+that\s+(.+)$`)},
	{factList, "pt", regexp.MustCompile(`(?i)^(?:o\s+que|oq|oque)\s+(?:voc[eê]|vc)\s+(?:(?:sabe|lembra)\s+(?:sobre|de)\s+mim|guardou(?:\s+(?:sobre|de)\s+mim)?)\s*\??$`)},
	{factList, "en", regexp.MustCompile(`(?i)^what\s+(?:do\s+you\s+know\s+about\s+me|have\s+you\s+(?:saved|kept)(?:\s+about\s+me)?)\s*\??$`)},
	{factForget, "pt", regexp.MustCompile(`(?i)^(?:esque[cç]a|esquece)\s+(\d+)\s*$`)},
	{factForget, "pt", regexp.MustCompile(`(?is)^(?:esque[cç]a|esquece)\s+isso\b` + factSeparator + `(.*)$`)},
	{factForget, "en", regexp.MustCompile(`(?i)^forget\s+(\d+)\s*$`)},
	{factForget, "en", regexp.MustCompile(`(?is)^forget\s+this\b` + factSeparator + `(.*)$`)},
}

// parseFactCommand reads a phrase. A question is never a request to keep or forget something: "lembra
// disso?" asks whether Ningo remembers.
func parseFactCommand(content string) (factCommand, string, string, bool) {
	content = strings.TrimSpace(factNameCall.ReplaceAllString(content, ""))
	for _, phrase := range factPhrases {
		match := phrase.pattern.FindStringSubmatch(content)
		if match == nil || (phrase.command != factList && strings.HasSuffix(content, "?")) {
			continue
		}
		argument := ""
		if len(match) > 1 {
			argument = strings.TrimSpace(match[1])
		}
		return phrase.command, phrase.locale, argument, true
	}
	return 0, "", "", false
}

// factSpeaker says whose facts these are: the owner on any verified channel, or a person the owner
// listed, recognized by the account and never by what the message says.
func factSpeaker(event core.Event) (string, bool) {
	if _, owner := ownerChannel(event); owner {
		return conversation.OwnerPerson, true
	}
	if name, _ := event.Context["known_speaker"].(string); name != "" {
		return conversation.PersonKey(name), true
	}
	return "", false
}

// factPlace names where the turn happens. Only what the person alone reads is private.
func factPlace(event core.Event) string {
	if _, owner := conversation.ScopeFor(string(event.Source)); owner || selfChatRequest(event) {
		return conversation.PlacePrivate
	}
	return string(event.Source) + ":" + event.Channel.ID
}

// WithFacts gives the handler what known people asked to keep.
func (h *ChatDomainHandler) WithFacts(facts *conversation.Facts) *ChatDomainHandler {
	h.facts = facts
	return h
}

// attachKnownFacts gives the turn what its speaker kept and may appear here. Without it a person who
// asks "what is my favorite film" in their own words would get a guess.
func (h *ChatDomainHandler) attachKnownFacts(ctx context.Context, event core.Event) {
	person, known := factSpeaker(event)
	if h.facts == nil || !known {
		return
	}
	event.Context["facts_available"] = true
	facts, err := h.facts.Recall(ctx, person, factPlace(event))
	if err != nil {
		logger.Error("read kept facts", err)
		return
	}
	if kept := conversation.ContextFacts(facts); len(kept) > 0 {
		event.Context["known_facts"] = kept
	}
}

// factReply answers a phrase without asking the model, so what is said to have been kept was kept.
func (h *ChatDomainHandler) factReply(ctx context.Context, event core.Event, content string) (string, bool) {
	if _, voice := event.Context["voice_turns"]; voice {
		return "", false
	}
	command, locale, argument, ok := parseFactCommand(content)
	if !ok {
		return "", false
	}
	c := factCatalogs[locale]
	person, known := factSpeaker(event)
	if !known {
		return c.say("not_known"), true
	}
	if h.facts == nil {
		return c.say("unavailable"), true
	}
	place := factPlace(event)
	switch command {
	case factRemember:
		return h.rememberFact(ctx, c, person, place, argument), true
	case factList:
		return h.listFacts(ctx, c, person, place), true
	default:
		return h.forgetFact(ctx, c, person, place, argument), true
	}
}

func (h *ChatDomainHandler) rememberFact(ctx context.Context, c factCatalog, person, place, argument string) string {
	if argument == "" {
		return c.say("remember_how")
	}
	fact, err := h.facts.Remember(ctx, person, place, argument)
	switch {
	case err == nil:
		return c.say("remembered", "fact", fact.Content)
	case errors.Is(err, conversation.ErrTooManyFacts):
		return c.say("too_many", "max", strconv.Itoa(conversation.MaxFactsPerPerson))
	case errors.Is(err, conversation.ErrInvalid):
		return c.say("too_long", "max", strconv.Itoa(conversation.MaxFactRunes))
	}
	logger.Error("keep a fact", err)
	return c.say("unavailable")
}

func (h *ChatDomainHandler) listFacts(ctx context.Context, c factCatalog, person, place string) string {
	facts, err := h.facts.Recall(ctx, person, place)
	if err != nil {
		logger.Error("read kept facts", err)
		return c.say("unavailable")
	}
	if len(facts) == 0 {
		return c.say("nothing_kept")
	}
	lines := []string{c.say("list_header")}
	for index, fact := range facts {
		lines = append(lines, c.say("list_item", "n", strconv.Itoa(index+1), "fact", fact.Content))
	}
	return strings.Join(append(lines, "", c.say("list_footer")), "\n")
}

// forgetFact finds the fact by its number in the list or by words only one of them holds. Only what
// the list shows here can be forgotten here, so a shared chat cannot reach what was kept in private.
func (h *ChatDomainHandler) forgetFact(ctx context.Context, c factCatalog, person, place, argument string) string {
	if argument == "" {
		return c.say("forget_how")
	}
	facts, err := h.facts.Recall(ctx, person, place)
	if err != nil {
		logger.Error("read kept facts", err)
		return c.say("unavailable")
	}
	found := []conversation.Fact{}
	if number, err := strconv.Atoi(argument); err == nil {
		if number >= 1 && number <= len(facts) {
			found = append(found, facts[number-1])
		}
	} else {
		words := strings.ToLower(argument)
		for _, fact := range facts {
			if strings.Contains(strings.ToLower(fact.Content), words) {
				found = append(found, fact)
			}
		}
	}
	switch len(found) {
	case 0:
		return c.say("forget_none")
	case 1:
		if err := h.facts.Delete(ctx, found[0].ID); err != nil {
			logger.Error("delete a kept fact", err)
			return c.say("unavailable")
		}
		return c.say("forgotten", "fact", found[0].Content)
	default:
		return c.say("forget_many", "count", strconv.Itoa(len(found)))
	}
}

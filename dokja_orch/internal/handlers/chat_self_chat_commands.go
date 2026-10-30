package handlers

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	conversation "read_books/dokja_domain/dokja_conversation"
)

// The owner manages the phone's conversation from the WhatsApp self-chat with a few commands. Only the
// self-chat accepts them: contacts and groups write plain text, never commands. A command is not a turn
// of any conversation. Each command has a Portuguese and an English name, and the answer is in the
// language of the name used.

//go:embed locales/self_chat_*.json
var selfChatLocales embed.FS

type selfChatCatalog map[string]string

var selfChatCatalogs = func() map[string]selfChatCatalog {
	catalogs := map[string]selfChatCatalog{}
	for _, locale := range []string{"pt", "en"} {
		raw, err := selfChatLocales.ReadFile("locales/self_chat_" + locale + ".json")
		if err != nil {
			panic(err)
		}
		catalog := selfChatCatalog{}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			panic(fmt.Sprintf("self-chat catalog %s: %v", locale, err))
		}
		catalogs[locale] = catalog
	}
	return catalogs
}()

// say fills a catalog message's {name} placeholders.
func (c selfChatCatalog) say(key string, values ...string) string {
	text := c[key]
	for i := 0; i+1 < len(values); i += 2 {
		text = strings.ReplaceAll(text, "{"+values[i]+"}", values[i+1])
	}
	return text
}

type selfChatCommand string

const (
	commandNew    selfChatCommand = "new"
	commandList   selfChatCommand = "list"
	commandOpen   selfChatCommand = "open"
	commandRename selfChatCommand = "rename"
	commandDelete selfChatCommand = "delete"
	commandYes    selfChatCommand = "yes"
	commandHelp   selfChatCommand = "help"
)

var selfChatCommandNames = map[string]struct {
	command selfChatCommand
	locale  string
}{
	"/nova": {commandNew, "pt"}, "/new": {commandNew, "en"},
	"/conversas": {commandList, "pt"}, "/conversations": {commandList, "en"},
	"/abrir": {commandOpen, "pt"}, "/open": {commandOpen, "en"},
	"/renomear": {commandRename, "pt"}, "/rename": {commandRename, "en"},
	"/apagar": {commandDelete, "pt"}, "/delete": {commandDelete, "en"},
	"/sim": {commandYes, "pt"}, "/yes": {commandYes, "en"},
	"/ajuda": {commandHelp, "pt"}, "/help": {commandHelp, "en"},
}

// How many conversations a list shows, how long its numbers stay valid, and how long a delete waits
// for its confirmation.
const (
	selfChatListSize     = 8
	selfChatListLifetime = 10 * time.Minute
	selfChatConfirmTime  = 2 * time.Minute
)

type listedConversation struct {
	id       string
	revision uint64
	title    string
}

// selfChatState is what the commands remember between messages, in memory: the numbers of the last
// list and a delete waiting for its confirmation. A restart forgets both; the conversation the phone
// continues is kept in the archive.
type selfChatState struct {
	listed   []listedConversation
	listedAt time.Time
	pending  *listedConversation
	askedAt  time.Time
}

// parseSelfChatCommand reads a command: a known name, alone or followed by its argument. Anything
// else, a message that merely starts with a slash included, is an ordinary message.
func parseSelfChatCommand(content string) (selfChatCommand, string, string, bool) {
	name, argument, _ := strings.Cut(strings.TrimSpace(content), " ")
	known, ok := selfChatCommandNames[strings.ToLower(name)]
	if !ok {
		return "", "", "", false
	}
	return known.command, known.locale, strings.Join(strings.Fields(argument), " "), true
}

// selfChatCommandReply runs a command and gives the answer to send back. It runs under the self-chat
// lock, so it never races the choice of the conversation a message continues.
func (h *ChatDomainHandler) selfChatCommandReply(ctx context.Context, scope conversation.Scope, command selfChatCommand, locale, argument string) string {
	c := selfChatCatalogs[locale]
	now := time.Now()
	state := &h.selfChatState
	numbered := func() (listedConversation, string) {
		if len(state.listed) == 0 || now.Sub(state.listedAt) > selfChatListLifetime {
			return listedConversation{}, c.say("list_needed")
		}
		n, err := strconv.Atoi(argument)
		if err != nil || n < 1 || n > len(state.listed) {
			return listedConversation{}, c.say("unknown_number", "n", argument)
		}
		return state.listed[n-1], ""
	}
	failed := func(err error) string {
		if errors.Is(err, conversation.ErrConflict) || errors.Is(err, conversation.ErrNotFound) {
			state.listed = nil
			return c.say("changed")
		}
		return c.say("unavailable")
	}
	titled := func(title string) string {
		if title == "" {
			return c.say("untitled")
		}
		return title
	}

	switch command {
	case commandNew:
		if _, err := h.archive.Start(ctx, scope, conversation.KindGeneral, "", ""); err != nil {
			return failed(err)
		}
		return c.say("new_started")
	case commandList:
		items, _, err := h.archive.List(ctx, scope, "", conversation.MaxListLimit)
		if err != nil {
			return failed(err)
		}
		current, _, _ := h.archive.Continued(ctx, scope)
		state.listed, state.listedAt = nil, now
		lines := []string{c.say("list_header")}
		for _, item := range items {
			if item.Kind != conversation.KindGeneral || len(state.listed) == selfChatListSize {
				continue
			}
			state.listed = append(state.listed, listedConversation{id: item.ID, revision: item.Revision, title: item.Title})
			line := c.say("list_item", "n", strconv.Itoa(len(state.listed)), "title", titled(item.Title),
				"turns", strconv.Itoa(item.Turns), "date", item.UpdatedAt.Local().Format(c["date_layout"]))
			if item.ID == current.ID {
				line += c.say("list_current")
			}
			lines = append(lines, line)
		}
		if len(state.listed) == 0 {
			return c.say("list_empty")
		}
		return strings.Join(append(lines, "", c.say("list_footer")), "\n")
	case commandOpen:
		chosen, refusal := numbered()
		if refusal != "" {
			return refusal
		}
		if _, err := h.archive.Touch(ctx, scope, chosen.id, chosen.revision); err != nil {
			return failed(err)
		}
		state.listed = nil
		return c.say("opened", "title", titled(chosen.title))
	case commandRename:
		if argument == "" {
			return c.say("rename_empty")
		}
		if utf8.RuneCountInString(argument) > conversation.MaxTitleRunes {
			return c.say("rename_too_long")
		}
		current, found, err := h.archive.Continued(ctx, scope)
		if err != nil {
			return failed(err)
		}
		if !found {
			return c.say("no_current")
		}
		if _, err := h.archive.Rename(ctx, scope, current.ID, current.Revision, argument); err != nil {
			return failed(err)
		}
		state.listed = nil
		return c.say("renamed", "title", argument)
	case commandDelete:
		chosen, refusal := numbered()
		if refusal != "" {
			return refusal
		}
		state.pending, state.askedAt = &chosen, now
		return c.say("delete_ask", "title", titled(chosen.title))
	case commandYes:
		pending := state.pending
		state.pending = nil
		if pending == nil || now.Sub(state.askedAt) > selfChatConfirmTime {
			return c.say("nothing_to_confirm")
		}
		if err := h.archive.Delete(ctx, scope, pending.id, pending.revision); err != nil {
			return failed(err)
		}
		state.listed = nil
		return c.say("deleted", "title", titled(pending.title))
	default:
		return c.say("help")
	}
}

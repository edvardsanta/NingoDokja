package chat

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxMentions bounds the accounts one message can tag that Ningo resolves.
const MaxMentions = 20

const maxMentionNameRunes = 64

// Mention is a Discord account a message tags, with the display name the gateway shows for it.
// The name is chosen by the account, so it is only display information.
type Mention struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var userMention = regexp.MustCompile(`<@!?([0-9]{15,25})>`)

// ResolveMentions replaces each user tag, which is only a numeric account id, with a name. A person
// the owner configured is called by that name, resolved from the verified account id. Anyone else is
// called by the display name the gateway reported. A tag with neither becomes the account's
// participant alias, the same name it would carry if it spoke in this session. The account id never
// stays in the text, and nothing here grants any authority.
func ResolveMentions(scope, content string, mentions []Mention, known func(string) KnownName) string {
	if !strings.Contains(content, "<@") {
		return content
	}
	names := make(map[string]string, len(mentions))
	for _, mention := range mentions[:min(len(mentions), MaxMentions)] {
		if name := cleanMentionName(mention.Name); name != "" {
			names[mention.ID] = name
		}
	}
	return userMention.ReplaceAllStringFunc(content, func(tag string) string {
		id := userMention.FindStringSubmatch(tag)[1]
		if known != nil {
			if name := cleanMentionName(known(id).Name); name != "" {
				return "@" + name
			}
		}
		if name, found := names[id]; found {
			return "@" + name
		}
		return "@" + ParticipantAlias(scope, id)
	})
}

// cleanMentionName keeps a name to one short line of ordinary text, so a display name cannot carry a
// line break, a tag or markup into the message.
func cleanMentionName(name string) string {
	var cleaned strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), r == '@', r == '<', r == '>', r == '`':
		case unicode.IsSpace(r):
			cleaned.WriteRune(' ')
		default:
			cleaned.WriteRune(r)
		}
	}
	runes := []rune(strings.Join(strings.Fields(cleaned.String()), " "))
	return string(runes[:min(len(runes), maxMentionNameRunes)])
}

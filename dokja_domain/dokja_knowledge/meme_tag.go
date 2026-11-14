package knowledge

import (
	"net/url"
	"path"
	"strings"
	"unicode"
)

// maxMemeTagRunes bounds a tag proposed from a channel message.
const maxMemeTagRunes = 64

// MemeTagForm says how a channel message proposes a tag for a meme.
type MemeTagForm string

const (
	// MemeTagDirect is a post that carries the meme (a file or a link) and the tag.
	MemeTagDirect MemeTagForm = "direct"
	// MemeTagReply is a reply to a message that carries the meme, naming the tag.
	MemeTagReply MemeTagForm = "reply"
)

// MemeTagPost is a channel message read as a tag proposal.
type MemeTagPost struct {
	Hashtag string
	Form    MemeTagForm
	// Link is the first link in the message, when it has one.
	Link string
}

// ParseMemeTagPost decides whether a message in a meme channel proposes a tag. The rule is the
// domain's, so the routing that sends the message to intake and the authorization that lets it in
// without a mention read it the same way. A post proposes a tag when it carries a hashtag and
// the meme itself (an attachment or a link). A reply proposes one when it carries a hashtag and
// nothing else to read: the meme is the message it answers.
func ParseMemeTagPost(content string, hasMedia, isReply bool) (MemeTagPost, bool) {
	hashtag := FirstHashtag(content)
	if hashtag == "" {
		return MemeTagPost{}, false
	}
	link := FirstLink(content)
	switch {
	case hasMedia || link != "":
		return MemeTagPost{Hashtag: hashtag, Form: MemeTagDirect, Link: link}, true
	case isReply:
		return MemeTagPost{Hashtag: hashtag, Form: MemeTagReply}, true
	}
	return MemeTagPost{}, false
}

// FirstHashtag returns the first #label of a message, or "" when there is none. A # inside a word
// or a link (a page anchor) is not a tag.
func FirstHashtag(content string) string {
	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '#' || (i > 0 && !unicode.IsSpace(runes[i-1]) && !strings.ContainsRune("([{\"'", runes[i-1])) {
			continue
		}
		end := i + 1
		for end < len(runes) && (runes[end] == '_' || unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end])) {
			end++
		}
		if label := string(runes[i:end]); end > i+1 && end-i-1 <= maxMemeTagRunes {
			if _, err := PrepareHashtagExample("x", label); err == nil {
				return label
			}
		}
	}
	return ""
}

// FirstLink returns the first http(s) link of a message that is not a link to a chat message.
func FirstLink(content string) string {
	for _, field := range strings.Fields(content) {
		field = strings.Trim(field, "<>()[]\"',")
		address, err := url.Parse(field)
		if err != nil || address.Hostname() == "" || address.User != nil || (address.Scheme != "http" && address.Scheme != "https") {
			continue
		}
		if host := strings.ToLower(address.Hostname()); (host == "discord.com" || host == "discordapp.com") && strings.HasPrefix(address.Path, "/channels/") {
			continue
		}
		return field
	}
	return ""
}

var mediaFileExtensions = map[string]bool{
	".mp4": true, ".webm": true, ".mov": true, ".m4v": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// LinkIsMediaFile says whether a link points straight at an image or a video file, which can
// be read as it is, rather than at a page that has to be resolved first.
func LinkIsMediaFile(raw string) bool {
	address, err := url.Parse(raw)
	return err == nil && mediaFileExtensions[strings.ToLower(path.Ext(address.Path))]
}

// StableMediaRef is the identity address of a meme: the address without what a platform adds to
// make a link expire (the query and the fragment), so the same file always addresses the same
// example. A page link keeps its query, because the query can name the page.
func StableMediaRef(raw string) string {
	address, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || address.Hostname() == "" {
		return strings.TrimSpace(raw)
	}
	if LinkIsMediaFile(raw) {
		address.RawQuery, address.Fragment = "", ""
	} else {
		address.Fragment = ""
	}
	return address.String()
}

package chat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	codeBlock    = regexp.MustCompile("(?s)```.*?(```|$)")
	imageOrLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	citation     = regexp.MustCompile(`\[\d+(?:\s*,\s*\d+)*\]`)
	bareURL      = regexp.MustCompile(`(?i)\bhttps?://\S+`)
	htmlTag      = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	lineMarker   = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,6}[ \t]+|>[ \t]?|[-*+][ \t]+|\d+[.)][ \t]+)`)
	tableRule    = regexp.MustCompile(`(?m)^[ \t|:-]+$`)
	emphasis     = regexp.MustCompile("[*_~`]+")
	spaceRun     = regexp.MustCompile(`[ \t]*\n[ \t\n]*|[ \t]{2,}`)
	sentenceJoin = regexp.MustCompile(`([^.!?:;,\s])\s*\n`)
	looseMark    = regexp.MustCompile(`\s+([.,!?;:])`)
)

// unmarked keeps the words of a Markdown reply and drops what a voice would
// read as noise: code, link targets, citation markers, tags and syntax.
func unmarked(reply string) string {
	text := codeBlock.ReplaceAllString(reply, " ")
	text = imageOrLink.ReplaceAllString(text, "$1")
	text = citation.ReplaceAllString(text, "")
	text = bareURL.ReplaceAllString(text, "")
	text = htmlTag.ReplaceAllString(text, "")
	text = tableRule.ReplaceAllString(text, "")
	text = lineMarker.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "|", " ")
	text = emphasis.ReplaceAllString(text, "")
	// A list item or heading without its own punctuation still ends a thought.
	text = sentenceJoin.ReplaceAllString(text, "$1.\n")
	text = spaceRun.ReplaceAllString(text, " ")
	return strings.TrimSpace(looseMark.ReplaceAllString(text, "$1"))
}

// ActionSpeakReply reads one written Desktop reply aloud. The reply was already
// produced and kept by the normal conversation turn; this only prepares speech.
const ActionSpeakReply = "speak-reply"

// MaxSpeakReplyInput bounds the written reply a request may carry.
const MaxSpeakReplyInput = 32000

// MaxSpokenReply bounds what is synthesized. The full reply stays on screen, so
// a long written answer is read up to a sentence end within this budget.
const MaxSpokenReply = 600

// SpeakableReply turns a written reply into text for speech synthesis: markup
// and links are removed, and long replies end at the last complete sentence
// inside MaxSpokenReply. An empty result means there is nothing to say.
func SpeakableReply(reply string) (string, error) {
	if len([]rune(reply)) > MaxSpeakReplyInput {
		return "", fmt.Errorf("reply exceeds %d characters", MaxSpeakReplyInput)
	}
	spoken := unmarked(SpokenReply(reply))
	runes := []rune(spoken)
	if len(runes) <= MaxSpokenReply {
		return spoken, nil
	}
	cut := -1
	for i := MaxSpokenReply - 1; i > 0; i-- {
		if (runes[i] == '.' || runes[i] == '!' || runes[i] == '?') && unicode.IsSpace(runes[i+1]) {
			cut = i + 1
			break
		}
	}
	if cut <= 0 {
		// No sentence end in budget: stop at the last word instead of mid-word.
		cut = MaxSpokenReply
		for cut > 0 && !unicode.IsSpace(runes[cut]) {
			cut--
		}
		if cut == 0 {
			cut = MaxSpokenReply
		}
		return strings.TrimSpace(string(runes[:cut])) + "…", nil
	}
	return strings.TrimSpace(string(runes[:cut])), nil
}

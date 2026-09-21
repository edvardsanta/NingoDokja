package tui

import (
	"fmt"
	"strings"
)

// The interface text is written in English in the code. Each message is looked up in the
// catalog of the active language and shown as written when there is no entry, so English is
// both the default and the fallback. Add a language by adding a catalog and a case in
// SetLanguage.
//
// The language is process-wide and must be set before the program starts.

var language = "en"

// SetLanguage selects the interface language from a tag such as "pt", "pt-BR" or
// "pt_BR.UTF-8". Anything it does not know falls back to English.
func SetLanguage(tag string) {
	language = normalizeLanguage(tag)
}

// Language reports the active language code.
func Language() string { return language }

func normalizeLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "pt" || strings.HasPrefix(tag, "pt-") || strings.HasPrefix(tag, "pt_") || strings.HasPrefix(tag, "pt.") {
		return "pt"
	}
	return "en"
}

// DetectLanguage picks the language from, in order: DOKJA_LANG, LC_ALL, LC_MESSAGES, LANG.
func DetectLanguage(env func(string) string) string {
	for _, name := range []string{"DOKJA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(env(name)); value != "" && value != "C" && value != "POSIX" {
			return normalizeLanguage(value)
		}
	}
	return "en"
}

// tr returns the message in the active language. With arguments it formats them, so a
// translation must keep the same verbs in the same order as the English text.
func tr(message string, args ...any) string {
	text := message
	if language == "pt" {
		if translated, ok := ptMessages[message]; ok {
			text = translated
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

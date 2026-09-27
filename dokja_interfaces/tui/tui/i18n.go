package tui

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Translation catalogs are embedded so the TUI remains a single binary.
//
//go:embed locales/active.en.json
var englishCatalog []byte

//go:embed locales/active.pt-BR.json
var portugueseCatalog []byte

var (
	translations = newTranslationBundle()
	languageMu   sync.RWMutex
	activeLang   = "en"
	localizer    = i18n.NewLocalizer(translations, "en")
)

func newTranslationBundle() *i18n.Bundle {
	bundle := i18n.NewBundle(language.English)
	bundle.MustParseMessageFileBytes(englishCatalog, "active.en.json")
	bundle.MustParseMessageFileBytes(portugueseCatalog, "active.pt-BR.json")
	return bundle
}

// SetLanguage selects the interface language from a tag such as "pt", "pt-BR" or
// "pt_BR.UTF-8". Anything it does not know falls back to English. The language is
// selected at startup; the lock also keeps tests that switch it race-safe.
func SetLanguage(tag string) {
	selected := normalizeLanguage(tag)
	tags := []string{selected}
	if selected == "pt" {
		tags = []string{"pt-BR", "en"}
	}

	languageMu.Lock()
	activeLang = selected
	localizer = i18n.NewLocalizer(translations, tags...)
	languageMu.Unlock()
}

// Language reports the active language code.
func Language() string {
	languageMu.RLock()
	defer languageMu.RUnlock()
	return activeLang
}

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

// tr resolves a stable message ID from the active catalog. Formatting remains compatible
// with fmt.Printf so existing callers can pass positional values after the ID.
func tr(messageID string, args ...any) string {
	languageMu.RLock()
	current := localizer
	languageMu.RUnlock()

	text, err := current.Localize(&i18n.LocalizeConfig{MessageID: messageID})
	if err != nil {
		// Missing IDs are intentionally visible and are also rejected by catalog tests.
		text = messageID
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

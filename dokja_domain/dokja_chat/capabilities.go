package chat

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
)

//go:embed locales/capabilities_*.json
var capabilityLocales embed.FS

func capabilityGuidance() string { return promptfiles.MustRead("chat_capability_guidance") }

type capabilityEntry struct {
	ID    string `json:"id"`
	Usage string `json:"usage"`
}

type capabilityInfo struct {
	Description string   `json:"description"`
	Limits      []string `json:"limits"`
}

type capabilityCatalog struct {
	Version      int                          `json:"version"`
	Interfaces   map[string][]capabilityEntry `json:"interfaces"`
	Capabilities map[string]capabilityInfo    `json:"capabilities"`
}

// capabilityContexts holds one trimmed catalog per locale and interface: only
// the entries of the interface a conversation came from, plus the
// cross-interface "chat" entries. Shipping every interface's commands to every
// turn wastes prompt budget and invites the model to offer commands that do
// not exist where the speaker is.
var capabilityContexts, capabilityFallbackContext = buildCapabilityContexts()

func buildCapabilityContexts() (map[string]map[string]string, string) {
	contexts := map[string]map[string]string{}
	fallback := ""
	for _, locale := range []string{"pt", "en"} {
		raw, err := capabilityLocales.ReadFile("locales/capabilities_" + locale + ".json")
		if err != nil {
			panic(fmt.Sprintf("capability catalog %s: %v", locale, err))
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			panic(fmt.Sprintf("capability catalog %s: %v", locale, err))
		}
		var catalog capabilityCatalog
		if err := json.Unmarshal(compact.Bytes(), &catalog); err != nil {
			panic(fmt.Sprintf("capability catalog %s: %v", locale, err))
		}
		build := func(names ...string) string {
			interfaces := map[string][]capabilityEntry{}
			capabilities := map[string]capabilityInfo{}
			add := func(name string) {
				entries := catalog.Interfaces[name]
				interfaces[name] = entries
				for _, entry := range entries {
					capabilities[entry.ID] = catalog.Capabilities[entry.ID]
				}
			}
			for _, name := range names {
				add(name)
			}
			if len(catalog.Interfaces["chat"]) > 0 {
				add("chat")
			}
			var encoded bytes.Buffer
			encoder := json.NewEncoder(&encoded)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(capabilityCatalog{Version: catalog.Version, Interfaces: interfaces, Capabilities: capabilities}); err != nil {
				panic("invalid embedded capability catalog")
			}
			return strings.TrimSuffix(encoded.String(), "\n")
		}
		localeContexts := map[string]string{}
		for name := range catalog.Interfaces {
			if name == "chat" {
				continue
			}
			localeContexts[name] = build(name)
		}
		contexts[locale] = localeContexts
		fallback = build()
	}
	return contexts, fallback
}

// CapabilityContext supplies static self-knowledge independently of conversational memory,
// for the interface this turn came from ("discord", "whatsapp", "desktop", "cli"...) and the
// speaker's locale (a BCP 47 tag such as "pt-BR" or "en-US": "en*" selects the English
// catalog, anything else Portuguese). It describes access paths; it neither dispatches
// actions nor grants permissions. Unknown interfaces receive only the cross-interface
// entries.
func CapabilityContext(source, locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		if context, ok := capabilityContexts["en"][source]; ok {
			return capabilityGuidance() + "\n" + context
		}
		return capabilityGuidance() + "\n" + capabilityFallbackContext
	}
	if context, ok := capabilityContexts["pt"][source]; ok {
		return capabilityGuidance() + "\n" + context
	}
	return capabilityGuidance() + "\n" + capabilityFallbackContext
}

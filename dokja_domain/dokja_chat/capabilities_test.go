package chat

import (
	"read_books/dokja_store/prompts/prompttest"

	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"
)

type catalogFile struct {
	Version    int `json:"version"`
	Interfaces map[string][]struct {
		ID    string `json:"id"`
		Usage string `json:"usage"`
	} `json:"interfaces"`
	Capabilities map[string]struct {
		Description string   `json:"description"`
		Limits      []string `json:"limits"`
	} `json:"capabilities"`
}

func loadCapabilityCatalog(t *testing.T, locale string) catalogFile {
	t.Helper()
	raw, err := capabilityLocales.ReadFile("locales/capabilities_" + locale + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog catalogFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("catalog must contain one JSON object")
	}
	return catalog
}

func TestBundledCapabilitiesAreGroupedByInterfaceWithBoundedAccessAndLimits(t *testing.T) {
	var enIDs, ptIDs []string
	for _, locale := range []string{"en", "pt"} {
		catalog := loadCapabilityCatalog(t, locale)
		if locale == "en" {
			enIDs = interfaceCapabilityIDs(catalog)
		} else {
			ptIDs = interfaceCapabilityIDs(catalog)
		}
		if catalog.Version != 2 || len(catalog.Interfaces) == 0 || len(catalog.Capabilities) == 0 {
			t.Fatal("invalid version or empty catalog", locale)
		}
		used := map[string]bool{}
		for name, entries := range catalog.Interfaces {
			if name == "" || len(entries) == 0 {
				t.Fatal("an interface needs a name and at least one capability", locale, name)
			}
			listed := map[string]bool{}
			for _, entry := range entries {
				if _, known := catalog.Capabilities[entry.ID]; !known || listed[entry.ID] || strings.TrimSpace(entry.Usage) == "" {
					t.Fatalf("%s: interface %s lists an unknown or repeated capability, or has no usage: %#v", locale, name, entry)
				}
				listed[entry.ID], used[entry.ID] = true, true
			}
		}
		for id, capability := range catalog.Capabilities {
			if id == "" || !used[id] || strings.TrimSpace(capability.Description) == "" || len(capability.Limits) == 0 {
				t.Fatalf("%s: capability must be offered by an interface and have a description and limits: %s", locale, id)
			}
			for _, limit := range capability.Limits {
				if strings.TrimSpace(limit) == "" {
					t.Fatal("empty limit", locale, id)
				}
			}
		}
		for _, id := range []string{"music.playback", "music.history", "radio.playback", "conversation.voice", "memes.examples", "memory.facts", "quiz.take", "conversation.manage", "price.search", "price.compare"} {
			if _, ok := catalog.Capabilities[id]; !ok {
				t.Fatal("missing product capability", locale, id)
			}
		}
	}
	if strings.Join(enIDs, "|") != strings.Join(ptIDs, "|") {
		t.Fatal("locale catalogs must offer the same capabilities on the same interfaces")
	}
}

func interfaceCapabilityIDs(catalog catalogFile) []string {
	names := make([]string, 0, len(catalog.Interfaces))
	for name := range catalog.Interfaces {
		names = append(names, name)
	}
	// map iteration order is random; sort indirectly by marshaling each interface's ids.
	ids := make([]string, 0, len(names))
	for _, name := range names {
		entries := catalog.Interfaces[name]
		list := make([]string, 0, len(entries))
		for _, entry := range entries {
			list = append(list, entry.ID)
		}
		encoded, _ := json.Marshal(struct {
			Name    string   `json:"n"`
			Entries []string `json:"e"`
		}{name, list})
		ids = append(ids, string(encoded))
	}
	sort.Strings(ids)
	return ids
}

func TestCapabilityContextIsBoundedPerLocaleAndInterface(t *testing.T) {
	for _, locale := range []string{"pt", "en", "pt-BR", "en-US", ""} {
		for _, source := range []string{"cli", "tui", "desktop", "discord", "whatsapp", "unknown"} {
			if context := CapabilityContext(source, locale); len(context) > 12*1024 {
				t.Fatalf("excessive per-turn context for %s/%s: %d bytes", locale, source, len(context))
			}
		}
		if context := CapabilityContext("unknown-interface", locale); !strings.Contains(context, "memory.facts") {
			t.Fatalf("fallback context must keep the cross-interface entries for locale %s", locale)
		}
	}
}

func TestCapabilityContextSeparatesPlaybackHistoryFromTrainingAndExecution(t *testing.T) {
	context := CapabilityContext("discord", "en")
	for index, required := range []string{
		"not live service availability or tools granted to this chat",
		"Only actual operation results confirm execution or success",
		"conversation summaries cannot add capabilities",
		"/play <song or link>", "/history",
		"editable now-playing panel",
		"one song in three",
		"1st, 4th, 7th",
		"starts again after a restart",
		"not explicit likes or dislikes",
		"does not currently train a music recommendation model",
		"not listening history or a learned listener profile",
	} {
		if index < 3 && !prompttest.Installed {
			continue
		}
		if !strings.Contains(context, required) {
			t.Fatalf("missing capability limit %q", required)
		}
	}
}

func TestCapabilityContextFollowsTheSpeakerLocale(t *testing.T) {
	pt := CapabilityContext("discord", "pt-BR")
	for _, required := range []string{"/preco", "/comparar_produto", "/tocar <música ou link>", "/painel_ningo"} {
		if !strings.Contains(pt, required) {
			t.Fatalf("pt context missing %q", required)
		}
	}
	if strings.Contains(pt, "in Portuguese") || strings.Contains(pt, "/compare_product") {
		t.Fatal("pt context must carry the Portuguese command names without English annotations")
	}
	en := CapabilityContext("discord", "en-US")
	if !strings.Contains(en, "/compare_product") || !strings.Contains(en, "in Portuguese") {
		t.Fatal("en context must keep the English command names with the Portuguese annotations")
	}
	if !strings.Contains(CapabilityContext("discord", ""), "/comparar_produto") {
		t.Fatal("a missing locale defaults to the Portuguese catalog")
	}
}

func offers(catalog catalogFile, interfaceName, id string) bool {
	for _, entry := range catalog.Interfaces[interfaceName] {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func TestLinkReadingAdvertisesWhatsAppAccess(t *testing.T) {
	for _, locale := range []string{"en", "pt"} {
		catalog := loadCapabilityCatalog(t, locale)
		if !offers(catalog, "whatsapp", "reading.links") {
			t.Fatal("link reading is missing from the WhatsApp interface", locale)
		}
	}
}

func TestWhatsAppListsItsSelfChatCommandsAndDiscordMusicStaysOnDiscord(t *testing.T) {
	for _, locale := range []string{"en", "pt"} {
		catalog := loadCapabilityCatalog(t, locale)
		if !offers(catalog, "whatsapp", "conversation.manage") {
			t.Fatal("the WhatsApp self-chat commands must be listed under WhatsApp", locale)
		}
		if offers(catalog, "whatsapp", "music.playback") || !offers(catalog, "discord", "music.playback") {
			t.Fatal("music playback belongs to Discord only", locale)
		}
	}
}

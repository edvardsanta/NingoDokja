package chat

import (
	"bytes"
	"encoding/json"
	"io"
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

func TestBundledCapabilitiesAreGroupedByInterfaceWithBoundedAccessAndLimits(t *testing.T) {
	var catalog catalogFile
	decoder := json.NewDecoder(bytes.NewReader(capabilitiesJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("catalog must contain one JSON object")
	}
	if catalog.Version != 2 || len(catalog.Interfaces) == 0 || len(catalog.Capabilities) == 0 || len(CapabilityContext()) > 12*1024 {
		t.Fatal("invalid version, empty catalog or excessive per-turn context")
	}
	used := map[string]bool{}
	for name, entries := range catalog.Interfaces {
		if name == "" || len(entries) == 0 {
			t.Fatal("an interface needs a name and at least one capability", name)
		}
		listed := map[string]bool{}
		for _, entry := range entries {
			if _, known := catalog.Capabilities[entry.ID]; !known || listed[entry.ID] || strings.TrimSpace(entry.Usage) == "" {
				t.Fatalf("interface %s lists an unknown or repeated capability, or has no usage: %#v", name, entry)
			}
			listed[entry.ID], used[entry.ID] = true, true
		}
	}
	for id, capability := range catalog.Capabilities {
		if id == "" || !used[id] || strings.TrimSpace(capability.Description) == "" || len(capability.Limits) == 0 {
			t.Fatalf("capability must be offered by an interface and have a description and limits: %s", id)
		}
		for _, limit := range capability.Limits {
			if strings.TrimSpace(limit) == "" {
				t.Fatal("empty limit", id)
			}
		}
	}
	for _, id := range []string{"music.playback", "music.history", "radio.playback", "conversation.voice", "memes.examples", "memory.facts", "quiz.take", "conversation.manage"} {
		if _, ok := catalog.Capabilities[id]; !ok {
			t.Fatal("missing product capability", id)
		}
	}
}

func TestCapabilityContextSeparatesPlaybackHistoryFromTrainingAndExecution(t *testing.T) {
	context := CapabilityContext()
	for _, required := range []string{
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
		if !strings.Contains(context, required) {
			t.Fatalf("missing capability limit %q", required)
		}
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
	var catalog catalogFile
	if err := json.Unmarshal(capabilitiesJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	if !offers(catalog, "whatsapp", "reading.links") {
		t.Fatal("link reading is missing from the WhatsApp interface")
	}
}

func TestWhatsAppListsItsSelfChatCommandsAndDiscordMusicStaysOnDiscord(t *testing.T) {
	var catalog catalogFile
	if err := json.Unmarshal(capabilitiesJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	if !offers(catalog, "whatsapp", "conversation.manage") {
		t.Fatal("the WhatsApp self-chat commands must be listed under WhatsApp")
	}
	if offers(catalog, "whatsapp", "music.playback") || !offers(catalog, "discord", "music.playback") {
		t.Fatal("music playback belongs to Discord only")
	}
}

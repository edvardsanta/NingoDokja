package chat

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBundledCapabilitiesHaveVersionedBoundedAccessAndLimits(t *testing.T) {
	var catalog struct {
		Version      int `json:"version"`
		Capabilities []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			EntryPoints []struct {
				Interface string `json:"interface"`
				Usage     string `json:"usage"`
			} `json:"entry_points"`
			Limits []string `json:"limits"`
		} `json:"capabilities"`
	}
	decoder := json.NewDecoder(bytes.NewReader(capabilitiesJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("catalog must contain one JSON object")
	}
	if catalog.Version != 1 || len(catalog.Capabilities) == 0 || len(CapabilityContext()) > 12*1024 {
		t.Fatal("invalid version, empty catalog or excessive per-turn context")
	}
	seen := map[string]bool{}
	for _, capability := range catalog.Capabilities {
		if capability.ID == "" || seen[capability.ID] || strings.TrimSpace(capability.Description) == "" || len(capability.EntryPoints) == 0 || len(capability.Limits) == 0 {
			t.Fatalf("capability must have unique identity, description, access and limits: %#v", capability)
		}
		seen[capability.ID] = true
		for _, entry := range capability.EntryPoints {
			if entry.Interface == "" || strings.TrimSpace(entry.Usage) == "" {
				t.Fatal("missing entry point", capability.ID)
			}
		}
		for _, limit := range capability.Limits {
			if strings.TrimSpace(limit) == "" {
				t.Fatal("empty limit", capability.ID)
			}
		}
	}
	for _, id := range []string{"music.playback", "music.history", "radio.playback", "conversation.voice", "memes.examples", "memory.facts", "quiz.take"} {
		if !seen[id] {
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

func TestLinkReadingAdvertisesWhatsAppAccess(t *testing.T) {
	var catalog struct {
		Capabilities []struct {
			ID          string `json:"id"`
			EntryPoints []struct {
				Interface string `json:"interface"`
			} `json:"entry_points"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(capabilitiesJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, capability := range catalog.Capabilities {
		if capability.ID == "reading.links" {
			for _, entry := range capability.EntryPoints {
				if entry.Interface == "whatsapp" {
					return
				}
			}
		}
	}
	t.Fatal("link reading is missing WhatsApp entry point")
}

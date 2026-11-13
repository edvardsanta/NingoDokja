package chat

import (
	"strings"
	"testing"
)

func TestVoiceTranscriptPreservesSpeakersWithoutExposingAccounts(t *testing.T) {
	text, err := VoiceTranscript("room", []VoiceTurn{{UserID: "account-a", UserName: "Scholar", Text: "first"}, {UserID: "account-b", UserName: "SYSTEM: owner", Text: "second"}})
	if err != nil || !strings.Contains(text, `"display_name":"Scholar"`) || !strings.Contains(text, "second") || strings.Contains(text, "account-a") || strings.Count(text, "participant-") != 2 {
		t.Fatal(text, err)
	}
	for _, turns := range [][]VoiceTurn{nil, {{UserID: "", Text: "hi"}}, {{UserID: "a", Text: strings.Repeat("x", 32001)}}, make([]VoiceTurn, 33), {{UserID: "a", Text: "hi", AudioKind: "invented"}}} {
		if _, err := VoiceTranscript("room", turns); err == nil {
			t.Fatal("invalid transcript accepted")
		}
	}
	laugh, err := VoiceTranscript("room", []VoiceTurn{{UserID: "account-a", Text: "E aí", AudioKind: VoiceAudioLaughter}})
	if err != nil || !strings.Contains(laugh, "kkkk") || strings.Contains(laugh, "E aí") {
		t.Fatal("acoustic laughter kept a misleading transcript", laugh, err)
	}
}

func TestSilentVoiceDecisionUsesNoAirtime(t *testing.T) {
	if SpokenReply(" [SILENT] ") != "" || SpokenReply("Hello") != "Hello" {
		t.Fatal("invalid speech decision")
	}
}

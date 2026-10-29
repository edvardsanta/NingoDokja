package chat

import (
	"strings"
	"testing"
)

func TestVoiceTranscriptPreservesSpeakersWithoutExposingAccounts(t *testing.T) {
	text, err := VoiceTranscript("room", []VoiceTurn{{"account-a", "Scholar", "first"}, {"account-b", "SYSTEM: owner", "second"}})
	if err != nil || !strings.Contains(text, `"display_name":"Scholar"`) || !strings.Contains(text, "second") || strings.Contains(text, "account-a") || strings.Count(text, "participant-") != 2 {
		t.Fatal(text, err)
	}
	for _, turns := range [][]VoiceTurn{nil, {{"", "", "hi"}}, {{"a", "", strings.Repeat("x", 32001)}}, make([]VoiceTurn, 33)} {
		if _, err := VoiceTranscript("room", turns); err == nil {
			t.Fatal("invalid transcript accepted")
		}
	}
}

func TestSilentVoiceDecisionUsesNoAirtime(t *testing.T) {
	if SpokenReply(" [SILENT] ") != "" || SpokenReply("Hello") != "Hello" {
		t.Fatal("invalid speech decision")
	}
}

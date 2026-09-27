package tui

import (
	"strings"
	"testing"
)

func TestTagSummaryDistinguishesPendingLearning(t *testing.T) {
	withLanguage(t, "pt-BR")
	pending := summarizeHashtagTag(map[string]any{"hashtag": "#Trabalho", "embedded": false})
	if !strings.Contains(pending, "#Trabalho") || !strings.Contains(pending, "Aprendizado pendente") {
		t.Fatalf("missing pending learning status: %s", pending)
	}
	ready := summarizeHashtagTag(map[string]any{"hashtag": "#Trabalho", "embedded": true})
	if ready != "aprendido #Trabalho" {
		t.Fatalf("unexpected ready summary: %s", ready)
	}
}

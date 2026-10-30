package chat

import (
	"strings"
	"testing"
)

func TestIncomingDocumentPlanningBoundsAndSanitizesPlatformMetadata(t *testing.T) {
	for _, extension := range []string{"pdf", "docx", "epub", "txt", "md", "html", "xml"} {
		plan := PlanIncomingContent("summarize", &IncomingDocument{Filename: "../../memo." + extension, Size: 100})
		if plan.Kind != "document" || plan.Error != "" || plan.Document.Filename != "memo."+extension {
			t.Fatalf("invalid plan: %+v", plan)
		}
	}
	for _, tc := range []struct {
		name   string
		size   int64
		reason string
	}{{"app.exe", 1, "document_unsupported"}, {"memo.pdf", MaxIncomingDocumentBytes + 1, "document_too_large"}, {"memo.pdf", -1, "document_too_large"}} {
		if got := PlanIncomingContent("", &IncomingDocument{Filename: tc.name, Size: tc.size}); got.Error != tc.reason {
			t.Fatalf("expected %s, got %+v", tc.reason, got)
		}
	}
	for _, message := range []string{"hello", "meme refresh", "Please read https://example.com/post", "https://user:password@example.com/post"} {
		if PlanIncomingContent(message, nil).Kind != "" {
			t.Fatalf("ordinary text routed as content: %s", message)
		}
	}
	if PlanIncomingContent("https://example.com/post", nil).Kind != "post" {
		t.Fatal("standalone post link not detected")
	}
}

func TestDocumentSummaryTreatsDocumentAsDataAndDeclaresPartialInput(t *testing.T) {
	for _, locale := range []string{"en", "pt-BR"} {
		messages, partial := DocumentSummaryMessages("memo.pdf", strings.Repeat("x", MaxSummaryRunes+1), "focus on results", locale)
		if !partial || len(messages) != 2 || messages[0].Role != "system" || !strings.Contains(messages[0].Content, "untrusted") || !strings.Contains(messages[1].Content, `"partial":true`) {
			t.Fatal("summary context lost boundaries")
		}
		if (locale == "en") != strings.Contains(messages[0].Content, "English") {
			t.Fatal("summary locale ignored")
		}
	}
}

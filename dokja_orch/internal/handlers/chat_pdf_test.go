package handlers

import (
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"testing"
)

func TestPDFUploadsAndLinksShareTheFourSourceBudget(t *testing.T) {
	reader := &webReadingService{}
	g := &fakeChatGenerator{result: map[string]any{"reply": "A source-based answer. [1]"}}
	h := NewChatDomainHandler(g).WithWebReading(reader, nil)
	event := chatEvent("https://example.com/first https://example.com/second")
	event.Payload["locale"] = "en"
	var attachments []chat.WebAttachment
	for i := 0; i < 3; i++ {
		attachments = append(attachments, chat.WebAttachment{URL: fmt.Sprintf("https://example.com/%d.pdf", i), Filename: "paper.pdf", Size: 1})
	}
	event.Payload["document_attachments"] = attachments
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(reader.calls) != chat.MaxWebLinks || !strings.Contains(result["reply"].(string), "1 additional") {
		t.Fatal("mixed-source budget not enforced", result, reader.calls, err)
	}
}

func TestOversizedPDFRefusesBeforeFetchingOrGenerating(t *testing.T) {
	reader := &webReadingService{}
	g := &presentationCaptureGenerator{}
	h := NewChatDomainHandler(g).WithWebReading(reader, nil)
	event := chatEvent("")
	event.Payload["document_attachments"] = []chat.WebAttachment{{URL: "https://example.com/large.pdf", Filename: "large.pdf", Size: chat.MaxWebDocumentBytes + 1}}
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(reader.calls) != 0 || len(g.messages) != 0 || !strings.Contains(result["reply"].(string), "25 MiB") {
		t.Fatal("oversized PDF reached inference or reader", result, err)
	}
}

func TestPDFExtractionFailureDoesNotGenerateFromFilename(t *testing.T) {
	reader := &webReadingService{fail: true, failureReason: "web_pdf_unreadable: private parser details"}
	g := &presentationCaptureGenerator{}
	h := NewChatDomainHandler(g).WithWebReading(reader, nil)
	event := chatEvent("")
	event.Payload["document_attachments"] = []chat.WebAttachment{{URL: "https://example.com/scanned.pdf", Filename: "scanned.pdf", Size: 1}}
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(g.messages) != 0 || !strings.Contains(result["reply"].(string), "could not extract text") || strings.Contains(result["reply"].(string), "private parser") {
		t.Fatal("unread PDF became evidence", result, err)
	}
}

func TestPDFConfiguredLimitIsReportedBeforeFetching(t *testing.T) {
	t.Setenv("DOKJA_PDF_MAX_MIB", "3")
	reader := &webReadingService{}
	g := &presentationCaptureGenerator{}
	h := NewChatDomainHandler(g).WithWebReading(reader, nil)
	event := chatEvent("")
	event.Payload["document_attachments"] = []chat.WebAttachment{{URL: "https://example.com/report.pdf", Filename: "report.pdf", Size: (3 << 20) + 1}}
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(reader.calls) != 0 || len(g.messages) != 0 || !strings.Contains(result["reply"].(string), "3 MiB") {
		t.Fatal("configured PDF limit not enforced or reported", result, err)
	}
}

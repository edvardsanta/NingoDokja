package handlers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

type mathGenerator struct {
	fakeChatGenerator
	data      []byte
	renderErr error
	renders   int
}

func (g *mathGenerator) RenderMath(context.Context, string) ([]byte, error) {
	g.renders++
	return g.data, g.renderErr
}

func TestChatMathDeliversImageOrReadableFallback(t *testing.T) {
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	for _, renderErr := range []error{nil, errors.New("renderer unavailable")} {
		generator := &mathGenerator{fakeChatGenerator: fakeChatGenerator{result: map[string]any{"reply": "```math\nlatex: \\frac{x}{2}\ntext: x / 2\nimage: true\n```\n"}}, data: pngData.Bytes(), renderErr: renderErr}
		result, err := NewChatDomainHandler(generator).Handle(context.Background(), chatEvent("Show a formula image"), chatStep)
		if err != nil {
			t.Fatal(err)
		}
		if result["reply"] != "x / 2\n" {
			t.Fatalf("lost fallback: %#v", result["reply"])
		}
		attachments, _ := result["attachments"].([]chat.Attachment)
		wantAttachments := 0
		if renderErr == nil {
			wantAttachments = 1
		}
		if len(attachments) != wantAttachments || generator.renders != 1 {
			t.Fatal("unexpected rendering result")
		}
		if renderErr != nil && result["notice"] != "chat.math_render_failed" {
			t.Fatal("renderer failure was hidden")
		}
		if renderErr == nil && result["notice"] == "chat.math_render_failed" {
			t.Fatal("successful rendering reported a failure")
		}
	}
}

func TestChatMathDoesNotInterpretOtherInterfaces(t *testing.T) {
	generator := &mathGenerator{}
	event := chatEvent("formula")
	event.Source = core.SourceDesktop
	input := "```math\nlatex: x\ntext: x\nimage: true\n```"
	presentation := NewChatDomainHandler(generator).presentMath(context.Background(), event, input)
	if presentation.text != input || len(presentation.attachments) != 0 || presentation.failed || generator.renders != 0 {
		t.Fatal("Discord presentation leaked to another interface")
	}
}

func TestMathFallbackReportsMissingRendererAndInvalidPNG(t *testing.T) {
	block := "```math\nlatex: x\ntext: x\nimage: true\n```\n"
	for _, generator := range []ChatGenerator{
		&fakeChatGenerator{result: map[string]any{"reply": block}},
		&mathGenerator{fakeChatGenerator: fakeChatGenerator{result: map[string]any{"reply": block}}, data: []byte("invalid PNG")},
	} {
		event := chatEvent("Show a formula image")
		event.Context = map[string]any{"force_new_session": true}
		result, err := NewChatDomainHandler(generator).Handle(context.Background(), event, chatStep)
		if err != nil {
			t.Fatal(err)
		}
		if result["notice"] != "chat.math_render_failed" || result["reply"] != "x\n" || result["attachments"] != nil {
			t.Fatalf("image failure was hidden or discarded text: %#v", result)
		}
	}
}

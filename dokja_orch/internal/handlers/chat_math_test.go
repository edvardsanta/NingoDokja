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
	}
}

func TestChatMathDoesNotInterpretOtherInterfaces(t *testing.T) {
	generator := &mathGenerator{}
	event := chatEvent("formula")
	event.Source = core.SourceDesktop
	input := "```math\nlatex: x\ntext: x\nimage: true\n```"
	text, attachments := NewChatDomainHandler(generator).presentMath(context.Background(), event, input)
	if text != input || len(attachments) != 0 || generator.renders != 0 {
		t.Fatal("Discord presentation leaked to another interface")
	}
}

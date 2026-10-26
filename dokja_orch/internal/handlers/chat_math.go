package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

type ChatMathRenderer interface {
	RenderMath(context.Context, string) ([]byte, error)
}

type mathPresentation struct {
	text        string
	attachments []chat.Attachment
	failed      bool
}

func (h *ChatDomainHandler) presentMath(ctx context.Context, event core.Event, reply string) mathPresentation {
	presentation := mathPresentation{text: reply}
	if event.Source != core.SourceDiscord {
		return presentation
	}
	plan := chat.PlanMath(reply)
	presentation.text = plan.Text
	if len(plan.Images) == 0 {
		return presentation
	}
	renderer, ok := h.generator.(ChatMathRenderer)
	if !ok {
		presentation.failed = true
		return presentation
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var attachments []chat.Attachment
	for _, formula := range plan.Images {
		data, err := renderer.RenderMath(ctx, formula.LaTeX)
		if err != nil || len(data) > chat.MaxImageBytes {
			continue
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 2232 || config.Height > 1032 {
			continue
		}
		attachments = append(attachments, chat.Attachment{
			Filename:    fmt.Sprintf("formula-%d.png", len(attachments)+1),
			ContentType: "image/png",
			DataB64:     base64.StdEncoding.EncodeToString(data),
		})
	}
	presentation.attachments = attachments
	presentation.failed = len(attachments) < len(plan.Images)
	return presentation
}

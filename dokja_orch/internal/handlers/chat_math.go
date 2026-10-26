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

func (h *ChatDomainHandler) presentMath(ctx context.Context, event core.Event, reply string) (string, []chat.Attachment) {
	if event.Source != core.SourceDiscord {
		return reply, nil
	}
	plan := chat.PlanMath(reply)
	renderer, ok := h.generator.(ChatMathRenderer)
	if !ok || len(plan.Images) == 0 {
		return plan.Text, nil
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
	return plan.Text, attachments
}

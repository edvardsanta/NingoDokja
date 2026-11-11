package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (c *ChatProviderClient) GenerateTaskText(ctx context.Context, profile string, messages []map[string]string, accept func(string) error) (map[string]any, error) {
	structured := append([]map[string]string(nil), messages...)
	structured = append(structured, map[string]string{"role": "system", "content": `Return one JSON object {"text":"your response"}. Preserve the requested language, persona and content requirements.`})
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"text"},
		"properties": map[string]any{"text": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}},
	}
	var text string
	reply, err := c.GenerateTaskJSON(ctx, profile, schema, structured, func(reply map[string]any) error {
		var decoded struct {
			Text string `json:"text"`
		}
		raw, _ := reply["reply"].(string)
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil || strings.TrimSpace(decoded.Text) == "" {
			return fmt.Errorf("missing task text")
		}
		if accept != nil {
			if err := accept(decoded.Text); err != nil {
				return err
			}
		}
		text = decoded.Text
		return nil
	})
	if err != nil {
		return nil, err
	}
	reply["reply"] = text
	return reply, nil
}

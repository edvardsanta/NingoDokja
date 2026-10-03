// Package communications defines which conversations the operator may read.
package communications

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const ActionChannels = "list-communication-channels"
const ActionHistory = "read-communication-history"

var snowflake = regexp.MustCompile(`^[0-9]{1,20}$`)

type Service interface {
	Dispatch(context.Context, string, map[string]any) (map[string]any, error)
}
type Domain struct {
	service  Service
	channels []string
}

func New(service Service, configured string) *Domain {
	d := &Domain{service: service}
	seen := map[string]bool{}
	for _, id := range strings.Split(configured, ",") {
		id = strings.TrimSpace(id)
		if snowflake.MatchString(id) && !seen[id] {
			d.channels = append(d.channels, id)
			seen[id] = true
		}
	}
	return d
}

func (d *Domain) Handle(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	if d == nil || d.service == nil {
		return nil, fmt.Errorf("communications service is not configured")
	}
	switch action {
	case ActionChannels:
		if len(d.channels) == 0 {
			return map[string]any{"channels": []any{}}, nil
		}
		return d.service.Dispatch(ctx, "channels", map[string]any{"channel_ids": d.channels})
	case ActionHistory:
		channel, _ := payload["channel_id"].(string)
		allowed := false
		for _, id := range d.channels {
			if channel == id {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("channel is not configured for communications")
		}
		before, _ := payload["before"].(string)
		if before != "" && !snowflake.MatchString(before) {
			return nil, fmt.Errorf("before must be a message id")
		}
		return d.service.Dispatch(ctx, "history", map[string]any{"channel_id": channel, "before": before, "limit": 30})
	default:
		return nil, fmt.Errorf("unsupported communications action")
	}
}

package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"dokja_interfaces/cli/cli"
)

type channelInfo struct {
	Meme     []string
	SafeOnly []string
}

func (c channelInfo) isSafeOnly(id string) bool {
	for _, safe := range c.SafeOnly {
		if safe == id {
			return true
		}
	}
	return false
}

// destinations are the channels an admin may send to: meme channels.
func (c channelInfo) destinations() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range c.Meme {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

type serviceRow struct {
	Name    string
	Status  string // ok, error, disabled or unchecked
	Enabled bool
	Detail  string
}

type jobRow struct {
	Name        string
	Enabled     bool
	Interval    string
	Override    bool
	NextAt      time.Time
	LastAt      time.Time
	LastOutcome string
	LastError   string
	AnnouncedAt time.Time
}

type profileRow struct {
	Name, BaseURL, Model, KeyHint string
	Active                        bool
}

type statusData struct {
	Profiles  []profileRow
	Services  []serviceRow
	Jobs      []jobRow
	Unsent    int
	Sent      int
	MemeOff   bool // the meme service is switched off, so its counts are unknown
	Channels  channelInfo
	UpdatedAt time.Time
}

func (s *statusData) activeProfile() (profileRow, bool) {
	for _, row := range s.Profiles {
		if row.Active {
			return row, true
		}
	}
	return profileRow{}, false
}

func (s *statusData) service(name string) (serviceRow, bool) {
	for _, row := range s.Services {
		if row.Name == name {
			return row, true
		}
	}
	return serviceRow{}, false
}

type memeItem struct {
	URL, Title, Tags, Source, DateCreated, DateSent string
}

type memePage struct {
	Items  []memeItem
	Total  int
	Offset int
}

type hashtagExample struct {
	SourceURL string
	Text      string
	Hashtag   string
	Embedded  bool
	UpdatedAt string
}

type hashtagExamples struct {
	Items  []hashtagExample
	Total  int
	Offset int
	Cursor int
}

type screenData struct {
	URL, Reason, Text string
	Safe              bool
	Detections        []string
}

type historyEntry struct {
	At      time.Time
	Label   string
	Summary string
	OK      bool
}

func str(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func num(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func strList(m map[string]any, key string) []string {
	out := []string{}
	switch v := m[key].(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func parseStatus(system, meme map[string]any, now time.Time) statusData {
	data := statusData{
		Unsent:    num(meme, "unsent_count"),
		Sent:      num(meme, "sent_count"),
		UpdatedAt: now,
	}
	if services, ok := system["services"].(map[string]any); ok {
		for _, name := range cli.ServiceNames {
			fields, ok := services[name].(map[string]any)
			if !ok {
				continue
			}
			row := serviceRow{Name: name, Status: str(fields, "status"), Detail: firstNonEmpty(str(fields, "error"), str(fields, "detail"))}
			row.Enabled, _ = fields["enabled"].(bool)
			data.Services = append(data.Services, row)
		}
	}
	if jobs, ok := system["jobs"].([]any); ok {
		for _, item := range jobs {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			row := jobRow{
				Name:        str(fields, "name"),
				Interval:    str(fields, "interval"),
				NextAt:      parseTime(str(fields, "next_at")),
				LastAt:      parseTime(str(fields, "last_at")),
				LastOutcome: str(fields, "last_outcome"),
				LastError:   str(fields, "last_error"),
				AnnouncedAt: parseTime(str(fields, "announced_at")),
			}
			row.Enabled, _ = fields["enabled"].(bool)
			row.Override, _ = fields["interval_override"].(bool)
			data.Jobs = append(data.Jobs, row)
		}
	}
	if profiles, ok := system["chat_profiles"].(map[string]any); ok {
		if rows, ok := profiles["profiles"].([]any); ok {
			for _, item := range rows {
				fields, ok := item.(map[string]any)
				if !ok {
					continue
				}
				row := profileRow{Name: str(fields, "name"), BaseURL: str(fields, "base_url"), Model: str(fields, "model"), KeyHint: str(fields, "key_hint")}
				row.Active, _ = fields["active"].(bool)
				data.Profiles = append(data.Profiles, row)
			}
		}
	}
	if channels, ok := system["channels"].(map[string]any); ok {
		data.Channels = channelInfo{
			Meme:     strList(channels, "meme"),
			SafeOnly: strList(channels, "safe_only"),
		}
	}
	return data
}

func parseMemePage(res map[string]any) memePage {
	page := memePage{Total: num(res, "total"), Offset: num(res, "offset")}
	if raw, ok := res["memes"].([]any); ok {
		for _, item := range raw {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			page.Items = append(page.Items, memeItem{
				URL:         str(fields, "url"),
				Title:       str(fields, "title"),
				Tags:        str(fields, "tags"),
				Source:      str(fields, "source"),
				DateCreated: str(fields, "date_created"),
				DateSent:    str(fields, "date_sent"),
			})
		}
	}
	return page
}

func parseScreen(res map[string]any) screenData {
	data := screenData{
		URL:    str(res, "url"),
		Reason: str(res, "reason"),
		Text:   str(res, "text"),
	}
	data.Safe, _ = res["safe"].(bool)
	if raw, ok := res["detections"].([]any); ok {
		for _, item := range raw {
			if fields, ok := item.(map[string]any); ok {
				data.Detections = append(data.Detections, fmt.Sprintf("%s %.2f", str(fields, "class"), fields["score"]))
			}
		}
	}
	return data
}

func summarizeDispatch(res map[string]any) string {
	summary := tr("d_meme_s_delivered", num(res, "delivered_count"))
	if skipped, ok := res["skipped_unsafe"].(map[string]any); ok && len(skipped) > 0 {
		parts := []string{}
		for channel, count := range skipped {
			parts = append(parts, fmt.Sprintf("%s: %v", channel, count))
		}
		sort.Strings(parts)
		summary += tr("skipped_by_nsfw_in") + strings.Join(parts, ", ")
	}
	return summary
}

func summarizeHashtagTag(res map[string]any) string {
	if embedded, ok := res["embedded"].(bool); ok && !embedded {
		return tr("hashtag_example_saved") + ": " + str(res, "hashtag") + ". " + tr("embedding_missing_help")
	}
	if hashtag := str(res, "hashtag"); hashtag != "" {
		return tr("learned") + hashtag
	}
	return tr("hashtag_example_saved")
}

func summarizeHashtagSuggestion(res map[string]any) string {
	hashtag := str(res, "hashtag")
	if hashtag == "" {
		return tr("no_hashtag_suggestion")
	}
	relevant, _ := res["relevant"].(bool)
	if !relevant {
		return tr("closest_is") + hashtag + tr("below_relevance_threshold")
	}
	return tr("suggested") + hashtag
}

func summarizeHashtagList(res map[string]any) string {
	return tr("d_hashtag_examples", num(res, "total"))
}

func parseHashtagExamples(res map[string]any) hashtagExamples {
	list := hashtagExamples{Total: num(res, "total"), Offset: num(res, "offset")}
	items, _ := res["examples"].([]any)
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		embedded, _ := fields["embedded"].(bool)
		list.Items = append(list.Items, hashtagExample{
			SourceURL: str(fields, "source_url"),
			Text:      str(fields, "text"),
			Hashtag:   str(fields, "hashtag"),
			Embedded:  embedded,
			UpdatedAt: str(fields, "updated_at"),
		})
	}
	return list
}

func summarizeSend(res map[string]any) string {
	summary := tr("sent_to") + strings.Join(strList(res, "sent_to"), ", ")
	if bypassed, _ := res["nsfw_bypassed"].(bool); bypassed {
		summary += tr("nsfw_bypass_summary")
	}
	if skipped := strList(res, "skipped_unsafe"); len(skipped) > 0 {
		summary += tr("skipped_in_s_s", strings.Join(skipped, ", "), str(res, "unsafe_reason"))
	}
	if marked, ok := res["marked_sent"].(bool); ok {
		if marked {
			summary += tr("marked_as_sent")
		} else {
			summary += tr("could_not_be_marked_as_sent")
		}
	}
	return summary
}

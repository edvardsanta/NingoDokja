package runtime

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Settings are the parts of the local stack's configuration the interfaces may change: which
// channels and people Ningo talks to, and the models it runs. Credentials are never settings;
// they stay in the owner's .env, edited by hand. Each value is checked against a strict pattern
// before it is written, because Compose reads it back into the stack.
type Setting struct {
	ID    string
	Group string
	// Env is the variable the setting is kept in.
	Env  string
	Kind string // list, enum, flag or text
	// Options are the values an enum accepts.
	Options []string
	// Affects are the modules that read the setting and are recreated when it changes.
	Affects []string
	// Note names a consequence the interfaces mention before saving (sessions, reindex).
	Note  string
	parse func(string) (string, error)
}

var (
	channelID = regexp.MustCompile(`^[0-9]{1,20}$`)
	groupID   = regexp.MustCompile(`^[0-9]+(-[0-9]+)*@g\.us$`)
	modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	phoneMark = regexp.MustCompile(`[\s().+-]`)
	phone     = regexp.MustCompile(`^[0-9]{7,15}$`)
)

var settingsCatalog = []Setting{
	{ID: "discord.meme_channels", Group: "discord", Env: "DISCORD_SCHEDULED_MEME_CHANNEL_ID", Kind: "list", Affects: []string{"orchestrator", "communications"}, Note: "sessions", parse: listOf(channel)},
	{ID: "discord.safe_only_channels", Group: "discord", Env: "DISCORD_SAFE_ONLY_CHANNEL_IDS", Kind: "list", Affects: []string{"orchestrator"}, Note: "sessions", parse: listOf(channel)},
	{ID: "discord.answer_channels", Group: "discord", Env: "DISCORD_ALLOWED_CHANNELS", Kind: "list", Affects: []string{"discord"}, parse: listOf(channel)},
	{ID: "discord.direct_messages", Group: "discord", Env: "DISCORD_DM_POLICY", Kind: "enum", Options: []string{"open", "closed"}, Affects: []string{"discord"}, parse: oneOf("open", "closed")},
	{ID: "whatsapp.people", Group: "whatsapp", Env: "DOKJA_WHATSAPP_ALLOW_FROM", Kind: "list", Affects: []string{"whatsapp"}, parse: listOf(phoneNumber)},
	{ID: "whatsapp.groups", Group: "whatsapp", Env: "DOKJA_WHATSAPP_ALLOWED_GROUPS", Kind: "list", Affects: []string{"whatsapp"}, parse: listOf(group)},
	{ID: "whatsapp.own_chat", Group: "whatsapp", Env: "DOKJA_WHATSAPP_SELF_CHAT", Kind: "flag", Options: []string{"true", "false"}, Affects: []string{"whatsapp"}, parse: oneOf("true", "false")},
	{ID: "models.embedding", Group: "models", Env: "DOKJA_EMBED_MODEL", Kind: "text", Affects: []string{"embedding_init", "memory", "knowledge"}, Note: "reindex", parse: model},
	{ID: "models.book_classifier", Group: "models", Env: "DOKJA_BOOK_CLASSIFIER_MODEL", Kind: "text", Affects: []string{"book"}, parse: model},
	{ID: "models.book_overview", Group: "models", Env: "DOKJA_BOOK_OVERVIEW_MODEL", Kind: "text", Affects: []string{"book"}, parse: model},
	{ID: "models.speech_to_text", Group: "models", Env: "DOKJA_VOICE_STT_MODEL", Kind: "text", Affects: []string{"voice"}, parse: model},
	{ID: "models.ocr", Group: "models", Env: "DOKJA_OCR_REC_MODEL", Kind: "text", Affects: []string{"meme"}, parse: model},
}

// SettingsEnv names every variable a setting may write, and the one listing webhook channels.
func SettingsEnv() []string {
	names := []string{WebhooksEnv}
	for _, s := range settingsCatalog {
		names = append(names, s.Env)
	}
	return names
}

// WebhooksEnv pairs channel ids with webhook addresses, which are credentials. Only the channel
// ids are ever read from it, to tell a webhook channel from a bot one.
const WebhooksEnv = "DOKJA_DISCORD_WEBHOOKS"

func channel(value string) (string, error) {
	if !channelID.MatchString(value) {
		return "", fmt.Errorf("not a channel id")
	}
	return value, nil
}

func phoneNumber(value string) (string, error) {
	digits := phoneMark.ReplaceAllString(value, "")
	if !phone.MatchString(digits) {
		return "", fmt.Errorf("not a phone number")
	}
	return digits, nil
}

func group(value string) (string, error) {
	if !groupID.MatchString(value) {
		return "", fmt.Errorf("not a group id (<digits>@g.us)")
	}
	return value, nil
}

func model(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" && !modelName.MatchString(value) {
		return "", fmt.Errorf("not a model name")
	}
	return value, nil
}

func oneOf(options ...string) func(string) (string, error) {
	return func(value string) (string, error) {
		value = strings.TrimSpace(value)
		for _, option := range options {
			if value == option {
				return value, nil
			}
		}
		return "", fmt.Errorf("must be one of %s", strings.Join(options, ", "))
	}
}

// listOf accepts comma-separated entries, normalizes each and drops repeats.
func listOf(entry func(string) (string, error)) func(string) (string, error) {
	return func(value string) (string, error) {
		var out []string
		seen := map[string]bool{}
		for i, raw := range strings.Split(value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			item, err := entry(raw)
			if err != nil {
				return "", fmt.Errorf("entry %d is %v", i+1, err)
			}
			if !seen[item] {
				seen[item] = true
				out = append(out, item)
			}
		}
		return strings.Join(out, ","), nil
	}
}

func lookupSetting(id string) (Setting, bool) {
	for _, s := range settingsCatalog {
		if s.ID == id {
			return s, true
		}
	}
	return Setting{}, false
}

// SettingsStore keeps the settings where the stack reads them. Read returns the current values by
// variable and the channel ids that deliver through a webhook; Write changes only the variables
// given.
type SettingsStore interface {
	Read() (values map[string]string, webhookChannels []string, err error)
	Write(values map[string]string) error
}

type SettingRow struct {
	ID      string   `json:"id"`
	Group   string   `json:"group"`
	Env     string   `json:"env"`
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
	Value   string   `json:"value"`
	Affects []string `json:"affects"`
	Note    string   `json:"note,omitempty"`
}

type SettingsView struct {
	Settings        []SettingRow `json:"settings"`
	WebhookChannels []string     `json:"webhook_channels"`
	Busy            bool         `json:"busy"`
	Error           string       `json:"error,omitempty"`
}

// WithSettings lets the domain read and change the stack's settings.
func (d *Domain) WithSettings(store SettingsStore) *Domain {
	d.settings = store
	return d
}

func (d *Domain) Settings() SettingsView {
	d.mu.Lock()
	busy, lastErr := d.busy, d.settingsErr
	d.mu.Unlock()
	view := SettingsView{Settings: []SettingRow{}, WebhookChannels: []string{}, Busy: busy, Error: lastErr}
	if d.settings == nil {
		view.Error = "settings are not available for this stack"
		return view
	}
	values, webhooks, err := d.settings.Read()
	if err != nil {
		view.Error = err.Error()
		return view
	}
	for _, s := range settingsCatalog {
		view.Settings = append(view.Settings, SettingRow{ID: s.ID, Group: s.Group, Env: s.Env, Kind: s.Kind, Options: s.Options, Value: values[s.Env], Affects: s.Affects, Note: s.Note})
	}
	if webhooks != nil {
		view.WebhookChannels = webhooks
	}
	return view
}

// SetSettings checks every change, then saves them and recreates the modules that read them in
// the background. Nothing is written when any value is refused.
func (d *Domain) SetSettings(changes map[string]string) (SettingsView, error) {
	if d.settings == nil {
		return SettingsView{}, fmt.Errorf("settings are not available for this stack")
	}
	if len(changes) == 0 {
		return SettingsView{}, fmt.Errorf("no setting to change")
	}
	current, _, err := d.settings.Read()
	if err != nil {
		return SettingsView{}, err
	}
	values := map[string]string{}
	affected := map[string]bool{}
	for id, raw := range changes {
		s, ok := lookupSetting(id)
		if !ok {
			return SettingsView{}, fmt.Errorf("unknown setting %q", id)
		}
		value, err := s.parse(raw)
		if err != nil {
			return SettingsView{}, fmt.Errorf("%s: %v", id, err)
		}
		values[s.Env] = value
		for _, name := range s.Affects {
			affected[name] = true
		}
	}
	merged := func(env string) string {
		if value, ok := values[env]; ok {
			return value
		}
		return current[env]
	}
	if merged("DOKJA_WHATSAPP_SELF_CHAT") == "false" && merged("DOKJA_WHATSAPP_ALLOW_FROM") == "" && merged("DOKJA_WHATSAPP_ALLOWED_GROUPS") == "" {
		return SettingsView{}, fmt.Errorf("whatsapp would answer nobody: keep its own chat, or list people or groups")
	}
	d.mu.Lock()
	if d.busy {
		d.mu.Unlock()
		return SettingsView{}, fmt.Errorf("another module operation is in progress")
	}
	d.busy, d.settingsErr = true, ""
	d.mu.Unlock()
	go d.applySettings(values, affected)
	return d.Settings(), nil
}

func (d *Domain) applySettings(values map[string]string, affected map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()
	err := d.settings.Write(values)
	if err == nil {
		err = d.recreate(ctx, affected)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.settingsErr = err.Error()
	}
	d.busy = false
}

// recreate brings running modules that read a changed setting back with it. A stopped container
// keeps its old environment, so it is marked to be recreated on its next start.
func (d *Domain) recreate(ctx context.Context, affected map[string]bool) error {
	containers, err := d.backend.Status(ctx)
	if err != nil {
		return err
	}
	for _, m := range inventory {
		if !affected[m.Name] {
			continue
		}
		state, exists := containers[m.Service]
		if !state.Running {
			if exists {
				if err := d.markStale(m.Service, true); err != nil {
					return err
				}
			}
			continue
		}
		d.setState(m, "starting", "")
		if err := d.backend.Recreate(ctx, m); err != nil {
			d.setState(m, "failed", err.Error())
			return err
		}
		if err := d.backend.Ready(ctx, m); err != nil {
			d.setState(m, "failed", err.Error())
			return err
		}
		d.setState(m, "ready", "")
		if err := d.markStale(m.Service, false); err != nil {
			return err
		}
	}
	return nil
}

func (d *Domain) setState(m Module, state, message string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.states[m.Name] = State{Name: m.Name, State: state, Error: message, Dependencies: m.Dependencies}
}

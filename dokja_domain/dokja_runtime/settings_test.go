package runtime

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

type memoryStore struct {
	mu       sync.Mutex
	values   map[string]string
	webhooks []string
	writes   int
}

func (s *memoryStore) Read() (map[string]string, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.values {
		out[k] = v
	}
	return out, s.webhooks, nil
}

func (s *memoryStore) Write(values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	for k, v := range values {
		s.values[k] = v
	}
	return nil
}

func withStore() (*Domain, *fake, *memoryStore) {
	d, f := fresh()
	store := &memoryStore{values: map[string]string{"DOKJA_WHATSAPP_SELF_CHAT": "true"}, webhooks: []string{"102"}}
	return d.WithSettings(store), f, store
}

func settleSettings(t *testing.T, d *Domain) SettingsView {
	t.Helper()
	settle(t, d)
	return d.Settings()
}

func TestSettingsAreNormalizedAndOnlyRunningReadersAreRecreated(t *testing.T) {
	d, f, store := withStore()
	change(t, d, "discord", true)
	before := len(f.trace())
	if _, err := d.SetSettings(map[string]string{"discord.answer_channels": " 7, 8,7 ", "whatsapp.people": "+55 (11) 91234-5678"}); err != nil {
		t.Fatal(err)
	}
	view := settleSettings(t, d)
	if view.Error != "" || store.values["DISCORD_ALLOWED_CHANNELS"] != "7,8" || store.values["DOKJA_WHATSAPP_ALLOW_FROM"] != "5511912345678" {
		t.Fatalf("%+v %v", view, store.values)
	}
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"recreate:discord"}) {
		t.Fatal("only the running module that reads a changed setting is recreated:", got)
	}
	if !reflect.DeepEqual(view.WebhookChannels, []string{"102"}) {
		t.Fatal(view.WebhookChannels)
	}
}

func TestARefusedValueWritesNothing(t *testing.T) {
	d, _, store := withStore()
	for _, changes := range []map[string]string{
		{"discord.direct_messages": "maybe"},
		{"whatsapp.groups": "123"},
		{"models.embedding": "bge-m3\nDISCORD_BOT_TOKEN=x"},
		{"discord.meme_channels": "101", "unknown.setting": "x"},
		{"whatsapp.own_chat": "false"},
	} {
		if _, err := d.SetSettings(changes); err == nil {
			t.Fatalf("%v must be refused", changes)
		}
	}
	if store.writes != 0 {
		t.Fatal("nothing may be written when a value is refused")
	}
}

func TestAStoppedReaderIsRecreatedOnItsNextStart(t *testing.T) {
	d, f, _ := withStore()
	f.mu.Lock()
	f.containers["dokja-book"] = Container{}
	f.mu.Unlock()
	if _, err := d.SetSettings(map[string]string{"models.book_overview": "org/model-name"}); err != nil {
		t.Fatal(err)
	}
	settleSettings(t, d)
	if strings.Contains(strings.Join(f.trace(), ","), "book") {
		t.Fatal("a stopped module is not started by a settings change")
	}
	change(t, d, "book", true)
	if trace := strings.Join(f.trace(), ","); !strings.Contains(trace, "recreate:book") || strings.Contains(trace, "start:book") {
		t.Fatal(trace)
	}
}

func TestSettingsListGroupsEverySettingWithItsReaders(t *testing.T) {
	d, _, _ := withStore()
	view := d.Settings()
	if len(view.Settings) != len(settingsCatalog) {
		t.Fatal(len(view.Settings))
	}
	for _, row := range view.Settings {
		if row.ID == "models.embedding" && (row.Note != "reindex" || len(row.Affects) != 3) {
			t.Fatalf("%+v", row)
		}
	}
	if (&Domain{}).Settings().Error == "" {
		t.Fatal("a stack without settings says so")
	}
}

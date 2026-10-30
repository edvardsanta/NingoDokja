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
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"recreate:orchestrator", "recreate:discord"}) {
		t.Fatal("both running readers of an inbound setting must be recreated:", got)
	}
	if !reflect.DeepEqual(view.WebhookChannels, []string{"102"}) {
		t.Fatal(view.WebhookChannels)
	}
}

func TestWhatsAppInboundSettingsRecreateBothRunningReaders(t *testing.T) {
	d, f, _ := withStore()
	change(t, d, "whatsapp", true)
	before := len(f.trace())
	if _, err := d.SetSettings(map[string]string{"whatsapp.groups": "123@g.us"}); err != nil {
		t.Fatal(err)
	}
	if got := settleSettings(t, d); got.Error != "" {
		t.Fatal(got.Error)
	}
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"recreate:orchestrator", "recreate:whatsapp"}) {
		t.Fatal("both running readers of an inbound setting must be recreated:", got)
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
	f.containers["dokja-feeds"] = Container{}
	f.mu.Unlock()
	if _, err := d.SetSettings(map[string]string{"models.book_overview": "org/model-name"}); err != nil {
		t.Fatal(err)
	}
	settleSettings(t, d)
	if strings.Contains(strings.Join(f.trace(), ","), "feeds") {
		t.Fatal("a stopped module is not started by a settings change")
	}
	change(t, d, "feeds", true)
	if trace := strings.Join(f.trace(), ","); !strings.Contains(trace, "recreate:feeds") || strings.Contains(trace, "start:feeds") {
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

func TestFailedRecreationKeepsEveryReaderStaleForRetry(t *testing.T) {
	d, f, _ := withStore()
	f.containers["dokja-orchestrator"] = Container{Running: true}
	f.containers["dokja-communications"] = Container{Running: true}
	f.fail = "orchestrator"
	if _, err := d.SetSettings(map[string]string{"discord.meme_channels": "101"}); err != nil {
		t.Fatal(err)
	}
	if got := settleSettings(t, d); got.Error == "" {
		t.Fatal("expected recreation failure")
	}
	if !d.isStale("dokja-orchestrator") || !d.isStale("dokja-communications") {
		t.Fatal("all readers must remain stale after partial failure")
	}
	f.fail = ""
	change(t, d, "communications", true)
	if got := strings.Join(f.trace(), ","); !strings.Contains(got, "recreate:communications") {
		t.Fatal(got)
	}
	if d.isStale("dokja-communications") {
		t.Fatal("successful retry must clear stale state")
	}
}

func TestEmbeddingProfileIsVisibleButCannotBeChanged(t *testing.T) {
	d, f, store := withStore()
	for _, row := range d.Settings().Settings {
		if row.ID == "models.embedding" && (!row.ReadOnly || row.Default != "bge-m3") {
			t.Fatal(row)
		}
	}
	if _, err := d.SetSettings(map[string]string{"models.embedding": "example/model", "discord.direct_messages": "closed"}); err == nil {
		t.Fatal("fixed profile accepted")
	}
	if store.writes != 0 || len(f.trace()) != 0 {
		t.Fatal("rejected batch changed the stack")
	}
}

func TestStaleCompletedInitializerIsRecreatedOnStart(t *testing.T) {
	d, f, _ := withStore()
	f.containers["dokja-embedding-init"] = Container{Completed: true}
	if err := d.markStale("dokja-embedding-init", true); err != nil {
		t.Fatal(err)
	}
	change(t, d, "memory", true)
	if got := strings.Join(f.trace(), ","); !strings.Contains(got, "recreate:embedding_init") {
		t.Fatal(got)
	}
}

func TestSettingsImportNormalizesOnlyKnownVariables(t *testing.T) {
	values, err := ImportSettings(map[string]string{"DISCORD_ALLOWED_CHANNELS": " 7,8,7 ", "DISCORD_BOT_TOKEN": "fixture-token"})
	if err != nil || values["discord.answer_channels"] != "7,8" || len(values) != len(settingsCatalog) {
		t.Fatal(values, err)
	}
	vars, err := SettingsVariables(values)
	if err != nil || vars["DISCORD_ALLOWED_CHANNELS"] != "7,8" {
		t.Fatal(vars, err)
	}
	if _, ok := vars["DISCORD_BOT_TOKEN"]; ok {
		t.Fatal("credential imported")
	}
	for _, bad := range []map[string]string{{"DOKJA_EMBED_MODEL": "other/model"}, {"DISCORD_DM_POLICY": "invalid"}} {
		if _, err := ImportSettings(bad); err == nil {
			t.Fatal("invalid legacy configuration accepted")
		}
	}
	values["unknown"] = "x"
	if _, err := SettingsVariables(values); err == nil {
		t.Fatal("unknown stored field accepted")
	}
}

type blockingSettingsStore struct {
	*memoryStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSettingsStore) Read() (map[string]string, []string, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.memoryStore.Read()
}
func TestSettingsReserveOperationBeforeReadingForValidation(t *testing.T) {
	d, _, store := withStore()
	blocked := &blockingSettingsStore{memoryStore: store, entered: make(chan struct{}), release: make(chan struct{})}
	d.WithSettings(blocked)
	done := make(chan error, 1)
	go func() { _, err := d.SetSettings(map[string]string{"discord.direct_messages": "closed"}); done <- err }()
	<-blocked.entered
	if _, err := d.Set("feeds", true); err == nil {
		t.Fatal("module changes must wait for settings validation")
	}
	if _, err := d.SetSettings(map[string]string{"whatsapp.own_chat": "false"}); err == nil {
		t.Fatal("concurrent settings must be refused before reading")
	}
	close(blocked.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	settleSettings(t, d)
}

func TestSettingsExposeEffectiveDefaults(t *testing.T) {
	d, _, _ := withStore()
	defaults := map[string]string{"discord.direct_messages": "open", "whatsapp.own_chat": "true", "models.embedding": "bge-m3"}
	for _, row := range d.Settings().Settings {
		if want, ok := defaults[row.ID]; ok && row.Default != want {
			t.Fatalf("%s default: %q, want %q", row.ID, row.Default, want)
		}
	}
}

func TestNamedConversationsAreCheckedLikeTheInterfaceChecksThem(t *testing.T) {
	good := "eu=+55 (11) 91234-5678,ningo=120363000000000001@g.us,team_1=123-456@g.us"
	got, err := conversations(good)
	if err != nil || got != "eu=5511912345678,ningo=120363000000000001@g.us,team_1=123-456@g.us" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{
		"no-separator",
		"=5511912345678",
		"Upper=5511912345678",
		"eu=not-a-number",
		"eu=$(reboot)",
		"eu=5511912345678,eu=5511987654321",
		"a=5511912345678,b=5511912345678",
		"a=1@g.us,b=1@g.us",
	} {
		if _, err := conversations(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestANewSettingIsImportedOnceAndUnrelatedLegacyValuesAreNotRead(t *testing.T) {
	stored, err := ImportSettings(map[string]string{"DOKJA_WHATSAPP_ALLOW_FROM": "5511912345678"})
	if err != nil {
		t.Fatal(err)
	}
	delete(stored, "whatsapp.conversations")
	stored["whatsapp.people"] = "5511900000000"

	merged, added, err := NewSettings(stored, map[string]string{
		"DOKJA_WHATSAPP_CONVERSATIONS": "eu=5511912345678",
		"DOKJA_WHATSAPP_ALLOW_FROM":    "this is no longer valid and must not matter",
	})
	if err != nil || !added || merged["whatsapp.conversations"] != "eu=5511912345678" || merged["whatsapp.people"] != "5511900000000" {
		t.Fatal(merged, added, err)
	}
	again, added, err := NewSettings(merged, map[string]string{"DOKJA_WHATSAPP_CONVERSATIONS": "other=5511987654321"})
	if err != nil || added || again["whatsapp.conversations"] != "eu=5511912345678" {
		t.Fatal("a stored setting must not be imported again", again, added, err)
	}
	if _, _, err := NewSettings(map[string]string{}, map[string]string{"DOKJA_WHATSAPP_CONVERSATIONS": "bad"}); err == nil {
		t.Fatal("an invalid value for a new setting must be refused")
	}
}

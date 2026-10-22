package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigurationPersistsWithoutChangingProfilesOrSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dokja.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveProfile(good("existing")); err != nil {
		t.Fatal(err)
	}
	c, err := db.InitializeRuntimeConfiguration(map[string]string{"discord.direct_messages": "open"})
	if err != nil {
		t.Fatal(err)
	}
	c, err = db.UpdateRuntimeConfiguration(c.Revision, map[string]string{"discord.direct_messages": "closed", "models.ocr": "fixture/model"})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.InitializeRuntimeConfiguration(map[string]string{"discord.direct_messages": "open"})
	if err != nil || got.Values["discord.direct_messages"] != "closed" || got.Revision != c.Revision {
		t.Fatal(got, err)
	}
	profiles, err := db.ListProfiles()
	if err != nil || len(profiles) != 1 || profiles[0].Name != "existing" {
		t.Fatal(profiles, err)
	}
	var version int
	db.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 1 {
		t.Fatal("schema version changed", version)
	}
}
func TestConcurrentConfigurationSaveDoesNotLoseAnUpdate(t *testing.T) {
	db := open(t)
	c, err := db.InitializeRuntimeConfiguration(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, connection := range []*DB{db, other} {
		wg.Add(1)
		go func(d *DB) {
			defer wg.Done()
			<-start
			_, err := d.UpdateRuntimeConfiguration(c.Revision, map[string]string{"models.ocr": "fixture/model"})
			results <- err
		}(connection)
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, conflict := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrConfigurationConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflict != 1 {
		t.Fatal(accepted, conflict)
	}
}
func TestInvalidConfigurationCannotReplaceStoredSnapshot(t *testing.T) {
	db := open(t)
	c, err := db.InitializeRuntimeConfiguration(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateRuntimeConfiguration(c.Revision, map[string]string{"": "invalid"}); err == nil {
		t.Fatal("invalid key accepted")
	}
	got, _, _ := db.RuntimeConfiguration()
	if got.Revision != c.Revision || len(got.Values) != 0 {
		t.Fatal(got)
	}
	db.db.Exec(`UPDATE settings SET value='not-json' WHERE key=?`, runtimeConfigurationKey)
	if _, _, err = db.RuntimeConfiguration(); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
}

func TestNullConfigurationValuesAreRejected(t *testing.T) {
	db := open(t)
	if _, err := db.InitializeRuntimeConfiguration(nil); err == nil {
		t.Fatal("nil snapshot accepted")
	}
	if _, err := db.db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)`, runtimeConfigurationKey, `{"version":1,"revision":1,"values":null}`, now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.RuntimeConfiguration(); err == nil {
		t.Fatal("null stored values accepted")
	}
	if _, err := db.UpdateRuntimeConfiguration(1, map[string]string{"models.ocr": "fixture/model"}); err == nil {
		t.Fatal("corrupt snapshot updated")
	}
}

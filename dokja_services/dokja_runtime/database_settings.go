package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
)

// DatabaseSettings is the infrastructure port shared by every operator client.
// The legacy .env is read for one-time import and webhook-channel projections;
// operational edits never rewrite it. DB contains stable setting IDs, not env keys.
type DatabaseSettings struct {
	DB     *store.DB
	Legacy EnvFile
	Lookup func(string) (string, bool)
	mu     sync.Mutex
}

func OpenDatabaseSettings(root string) (*DatabaseSettings, error) {
	path := os.Getenv("DOKJA_DB_FILE")
	if path == "" {
		path = filepath.Join(root, ".dokja", "dokja.db")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	db, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open runtime settings database")
	}
	return &DatabaseSettings{DB: db, Legacy: EnvFile{Path: filepath.Join(root, ".env")}, Lookup: os.LookupEnv}, nil
}
func (s *DatabaseSettings) Close() error { return s.DB.Close() }
func (s *DatabaseSettings) snapshot() (store.Configuration, []string, error) {
	legacy, webhooks, err := s.Legacy.Read()
	if err != nil {
		return store.Configuration{}, nil, err
	}
	c, exists, err := s.DB.RuntimeConfiguration()
	if err != nil {
		return c, nil, fmt.Errorf("cannot read runtime settings database")
	}
	if s.Lookup != nil {
		for _, name := range domain.SettingsEnv() {
			if name != domain.WebhooksEnv {
				if value, ok := s.Lookup(name); ok {
					legacy[name] = value
				}
			}
		}
	}
	if exists {
		// A setting added to the catalog since is imported once; after that the database decides it.
		values, added, err := domain.NewSettings(c.Values, legacy)
		if err != nil {
			return c, nil, err
		}
		if added {
			if c, err = s.DB.UpdateRuntimeConfiguration(c.Revision, values); err != nil {
				return c, nil, fmt.Errorf("cannot save runtime settings; reload before retrying")
			}
		}
		return c, webhooks, nil
	}
	values, err := domain.ImportSettings(legacy)
	if err != nil {
		return c, nil, err
	}
	c, err = s.DB.InitializeRuntimeConfiguration(values)
	if err != nil {
		return c, nil, fmt.Errorf("cannot initialize runtime settings database")
	}
	return c, webhooks, nil
}
func (s *DatabaseSettings) Read() (map[string]string, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, webhooks, err := s.snapshot()
	if err != nil {
		return nil, nil, err
	}
	values, err := domain.SettingsVariables(c.Values)
	return values, webhooks, err
}
func (s *DatabaseSettings) Write(changes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, _, err := s.snapshot()
	if err != nil {
		return err
	}
	// Translate the existing port's variable-keyed delta without allowing arbitrary
	// environment injection or storing the webhook credentials.
	vars, err := domain.SettingsVariables(c.Values)
	if err != nil {
		return err
	}
	for key, value := range changes {
		if _, ok := vars[key]; !ok || key == domain.WebhooksEnv {
			return fmt.Errorf("unknown runtime setting")
		}
		vars[key] = value
	}
	values, err := domain.ImportSettings(vars)
	if err != nil {
		return err
	}
	_, err = s.DB.UpdateRuntimeConfiguration(c.Revision, values)
	if err != nil {
		return fmt.Errorf("cannot save runtime settings; reload before retrying")
	}
	return nil
}

// Environment supplies a compatibility projection to existing Compose consumers.
// Saved values override only managed keys, including inherited shell overrides.
func (s *DatabaseSettings) Environment(base []string) ([]string, error) {
	values, _, err := s.Read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, managed := values[key]; !managed {
			out = append(out, entry)
		}
	}
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out, nil
}

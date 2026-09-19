package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const activeProfileKey = "chat_active_profile"

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Profile is what reads return: everything except the key itself.
type Profile struct {
	Name      string
	BaseURL   string
	Model     string
	KeyHint   string // "…abcd", the last four characters; empty when the key is too short to hint
	Active    bool
	UpdatedAt string
}

// NewProfile is the write side: the only place a key goes in.
type NewProfile struct {
	Name    string
	BaseURL string
	Model   string
	APIKey  string
}

func (p NewProfile) validate() error {
	if !profileName.MatchString(p.Name) {
		return fmt.Errorf("profile name must be 1-32 chars of a-z, 0-9, - or _ (got %q)", p.Name)
	}
	parsed, err := url.Parse(p.BaseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("base_url must be an http(s) URL (got %q)", p.BaseURL)
	}
	if strings.TrimSpace(p.Model) == "" || len(p.Model) > 200 {
		return fmt.Errorf("model is required (up to 200 chars)")
	}
	if p.APIKey == "" || len(p.APIKey) > 512 || strings.ContainsAny(p.APIKey, " \t\r\n") {
		return fmt.Errorf("token is required, up to 512 chars, without whitespace")
	}
	return nil
}

func keyHint(key string) string {
	if len(key) < 12 {
		return ""
	}
	return "…" + key[len(key)-4:]
}

// SaveProfile creates a profile or replaces an existing one of the same name.
func (d *DB) SaveProfile(p NewProfile) error {
	if err := p.validate(); err != nil {
		return err
	}
	stamp := now()
	_, err := d.db.Exec(
		`INSERT INTO chat_profiles(name, base_url, model, api_key, key_hint, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET base_url = excluded.base_url, model = excluded.model,
		   api_key = excluded.api_key, key_hint = excluded.key_hint, updated_at = excluded.updated_at`,
		p.Name, p.BaseURL, p.Model, p.APIKey, keyHint(p.APIKey), stamp, stamp)
	return err
}

// ListProfiles never selects the api_key column.
func (d *DB) ListProfiles() ([]Profile, error) {
	active, err := d.setting(activeProfileKey)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.Query("SELECT name, base_url, model, key_hint, updated_at FROM chat_profiles ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []Profile
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.Name, &p.BaseURL, &p.Model, &p.KeyHint, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Active = p.Name == active
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// ActiveProfile is the selected profile's name, or "" when none is selected.
func (d *DB) ActiveProfile() (string, error) { return d.setting(activeProfileKey) }

// UseProfile selects an existing profile. Selecting is all the orchestrator may do.
func (d *DB) UseProfile(name string) error {
	var exists int
	err := d.db.QueryRow("SELECT 1 FROM chat_profiles WHERE name = ?", name).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("unknown profile %q", name)
	}
	if err != nil {
		return err
	}
	return d.setSetting(activeProfileKey, name)
}

// DeleteProfile removes a profile. The active one needs force, because chat would then
// fall back to the environment settings.
func (d *DB) DeleteProfile(name string, force bool) error {
	active, err := d.setting(activeProfileKey)
	if err != nil {
		return err
	}
	if name == active && !force {
		return fmt.Errorf("profile %q is the active one; pick another first or use force", name)
	}
	result, err := d.db.Exec("DELETE FROM chat_profiles WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown profile %q", name)
	}
	if name == active {
		_, err = d.db.Exec("DELETE FROM settings WHERE key = ?", activeProfileKey)
	}
	return err
}

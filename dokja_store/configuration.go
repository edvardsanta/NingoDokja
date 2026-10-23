package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const runtimeConfigurationKey = "runtime.configuration.v1"
const providerConfigurationKey = "runtime.providers.v1"

var ErrConfigurationConflict = errors.New("configuration changed; reload before saving")

type Configuration struct {
	Version  int               `json:"version"`
	Revision uint64            `json:"revision"`
	Values   map[string]string `json:"values"`
}

func configurationJSON(c Configuration) (string, error) {
	if c.Version != 1 || c.Revision < 1 || c.Values == nil || len(c.Values) > 64 {
		return "", fmt.Errorf("invalid configuration snapshot")
	}
	for key, value := range c.Values {
		if key == "" || len(key) > 128 || len(value) > 4096 {
			return "", fmt.Errorf("invalid configuration entry")
		}
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > 32768 {
		return "", fmt.Errorf("configuration exceeds supported bounds")
	}
	return string(raw), nil
}
func decodeConfiguration(raw string) (Configuration, error) {
	var c Configuration
	if len(raw) > 32768 || json.Unmarshal([]byte(raw), &c) != nil {
		return c, fmt.Errorf("invalid stored configuration")
	}
	if _, err := configurationJSON(c); err != nil {
		return Configuration{}, err
	}
	return c, nil
}

// configuration reads only the dedicated key, never profiles or secrets.
// The existing settings table and user_version remain unchanged.
func (d *DB) configuration(key string) (Configuration, bool, error) {
	var raw string
	err := d.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return Configuration{}, false, nil
	}
	if err != nil {
		return Configuration{}, false, err
	}
	c, err := decodeConfiguration(raw)
	return c, true, err
}

// initializeConfiguration is insert-once: a later import cannot overwrite
// edits made by another controller/process or replace the first snapshot.
func (d *DB) initializeConfiguration(key string, values map[string]string) (Configuration, error) {
	raw, err := configurationJSON(Configuration{Version: 1, Revision: 1, Values: values})
	if err != nil {
		return Configuration{}, err
	}
	if _, err = d.db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, key, raw, now()); err != nil {
		return Configuration{}, err
	}
	c, _, err := d.configuration(key)
	return c, err
}
func (d *DB) updateConfiguration(key string, revision uint64, changes map[string]string) (Configuration, error) {
	var old string
	if err := d.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&old); err != nil {
		return Configuration{}, err
	}
	c, err := decodeConfiguration(old)
	if err != nil {
		return Configuration{}, err
	}
	if c.Revision != revision || revision == ^uint64(0) {
		return Configuration{}, ErrConfigurationConflict
	}
	for key, value := range changes {
		c.Values[key] = value
	}
	c.Revision++
	raw, err := configurationJSON(c)
	if err != nil {
		return Configuration{}, err
	}
	result, err := d.db.Exec(`UPDATE settings SET value=?,updated_at=? WHERE key=? AND value=?`, raw, now(), key, old)
	if err != nil {
		return Configuration{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Configuration{}, err
	}
	if count != 1 {
		return Configuration{}, ErrConfigurationConflict
	}
	return c, nil
}

// RuntimeConfiguration reads only runtime settings, never profiles or secrets.
func (d *DB) RuntimeConfiguration() (Configuration, bool, error) {
	return d.configuration(runtimeConfigurationKey)
}
func (d *DB) InitializeRuntimeConfiguration(values map[string]string) (Configuration, error) {
	return d.initializeConfiguration(runtimeConfigurationKey, values)
}
func (d *DB) UpdateRuntimeConfiguration(revision uint64, changes map[string]string) (Configuration, error) {
	return d.updateConfiguration(runtimeConfigurationKey, revision, changes)
}

// ProviderConfiguration uses an independent key and revision in the same table.
// It contains declarations, not observations or inference results.
func (d *DB) ProviderConfiguration() (Configuration, bool, error) {
	return d.configuration(providerConfigurationKey)
}
func (d *DB) InitializeProviderConfiguration(values map[string]string) (Configuration, error) {
	return d.initializeConfiguration(providerConfigurationKey, values)
}
func (d *DB) UpdateProviderConfiguration(revision uint64, changes map[string]string) (Configuration, error) {
	return d.updateConfiguration(providerConfigurationKey, revision, changes)
}

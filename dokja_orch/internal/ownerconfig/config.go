// Package ownerconfig loads private operator configuration independently of chat history.
package ownerconfig

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"time"
	"unicode"
)

const DefaultPath = ".dokja/owner.json"

type Config struct {
	Version int   `json:"version"`
	Owner   Owner `json:"owner"`
}
type Owner struct {
	ID       string            `json:"id"`
	Profile  chat.OwnerProfile `json:"profile"`
	Accounts Accounts          `json:"accounts"`
}
type Accounts struct {
	Discord DiscordAccount `json:"discord"`
}
type DiscordAccount struct {
	UserID string `json:"user_id"`
}

// Load uses the legacy account only when the file is absent. Invalid files never
// fall back. Error messages deliberately omit configuration values.
func Load(path, legacyDiscordID string) (Config, error) {
	if path == "" {
		path = DefaultPath
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return Config{Owner: Owner{Accounts: Accounts{Discord: DiscordAccount{UserID: strings.TrimSpace(legacyDiscordID)}}}}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open owner configuration: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return Config{}, fmt.Errorf("read owner configuration: %w", err)
	}
	if len(data) > 16384 {
		return Config{}, fmt.Errorf("owner configuration exceeds 16 KiB")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var config Config
	if err := dec.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("owner configuration must match the JSON schema")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("owner configuration must contain one JSON object")
	}
	if config.Version != 1 {
		return Config{}, fmt.Errorf("unsupported owner configuration version; expected 1")
	}
	if !validText(config.Owner.ID, 128) || !validText(config.Owner.Profile.Name, 128) {
		return Config{}, fmt.Errorf("owner id and profile name must be nonempty single-line text of at most 128 bytes")
	}
	if birthday := config.Owner.Profile.BirthDate; birthday != "" {
		date, err := time.Parse("2006-01-02", birthday)
		if err != nil || date.After(time.Now().UTC()) {
			return Config{}, fmt.Errorf("owner birth_date must be a valid past or present YYYY-MM-DD date")
		}
	}
	if len(config.Owner.Profile.Occupations) > 16 {
		return Config{}, fmt.Errorf("owner profile permits at most 16 occupations")
	}
	for _, occupation := range config.Owner.Profile.Occupations {
		if !validText(occupation, 128) {
			return Config{}, fmt.Errorf("owner occupations must be nonempty single-line text of at most 128 bytes")
		}
	}
	id := config.Owner.Accounts.Discord.UserID
	if len(id) < 17 || len(id) > 20 || strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return Config{}, fmt.Errorf("owner Discord user_id must contain 17 to 20 ASCII digits")
	}
	return config, nil
}

func validText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && value == strings.TrimSpace(value) && len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

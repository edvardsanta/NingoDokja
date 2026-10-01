package memory

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEndpoint     = "tcp://*:5562"
	DefaultDBFile       = "dokja_memory.db"
	DefaultEmbedTimeout = 5 * time.Second
	DefaultExpireAfter  = 30 * 24 * time.Hour
)

// Config is read from the environment, the way the other services are configured.
type Config struct {
	Endpoint      string
	DBFile        string
	EmbedEnabled  bool
	EmbedEndpoint string
	EmbedModel    string
	EmbedTimeout  time.Duration
	ExpireAfter   time.Duration
}

// ConfigFromEnv reads the MEMORY_* and DOKJA_EMBED_* variables. getenv is os.Getenv in
// production and a map lookup in tests.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	config := Config{
		Endpoint:      firstNonEmpty(getenv("MEMORY_SERVICE_ENDPOINT"), DefaultEndpoint),
		DBFile:        firstNonEmpty(getenv("MEMORY_DB_FILE"), DefaultDBFile),
		EmbedEnabled:  !switchedOff(getenv("DOKJA_EMBED")),
		EmbedEndpoint: firstNonEmpty(getenv("DOKJA_EMBED_ENDPOINT"), DefaultEmbedEndpoint),
		EmbedModel:    firstNonEmpty(getenv("DOKJA_EMBED_MODEL"), DefaultEmbedModel),
		EmbedTimeout:  DefaultEmbedTimeout,
		ExpireAfter:   DefaultExpireAfter,
	}
	if raw := strings.TrimSpace(getenv("DOKJA_EMBED_TIMEOUT")); raw != "" {
		seconds, err := strconv.ParseFloat(raw, 64)
		if err != nil || seconds <= 0 {
			return Config{}, fmt.Errorf("DOKJA_EMBED_TIMEOUT must be a positive number of seconds, got %q", raw)
		}
		config.EmbedTimeout = time.Duration(seconds * float64(time.Second))
	}
	if raw := strings.TrimSpace(getenv("MEMORY_EXPIRE_AFTER")); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration < 0 {
			return Config{}, fmt.Errorf("MEMORY_EXPIRE_AFTER must be a duration such as 720h (0 never expires), got %q", raw)
		}
		config.ExpireAfter = duration
	}
	return config, nil
}

// Build opens the database and wires the service. The returned function closes the database.
func Build(config Config, logger *log.Logger) (*Service, func(), error) {
	store, err := OpenStore(config.DBFile, config.ExpireAfter)
	if err != nil {
		return nil, nil, err
	}
	var embedder Embedder
	if config.EmbedEnabled {
		embedder = NewOllamaEmbedder(config.EmbedEndpoint, config.EmbedModel, config.EmbedTimeout)
	} else {
		logger.Printf("embeddings are disabled (DOKJA_EMBED=off); similarity will not work")
	}
	return NewService(store, embedder, config.EmbedModel), func() { store.Close() }, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func switchedOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "0", "false", "none":
		return true
	}
	return false
}

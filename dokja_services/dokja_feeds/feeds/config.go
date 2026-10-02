package feeds

import (
	"strings"
	"time"
)

const (
	DefaultEndpoint = "tcp://*:5563"

	// maxOutput bounds what one run of a plugin may print.
	maxOutput = 1 << 20

	// MaxItemsReturned bounds one feeds.items reply, however many plugins there are.
	MaxItemsReturned = 1000
)

// Limits are the ranges a plugin manifest may choose from.
type Limits struct {
	MinInterval time.Duration
	MaxInterval time.Duration
	MinTimeout  time.Duration
	MaxTimeout  time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MinInterval: time.Minute,
		MaxInterval: 24 * time.Hour,
		MinTimeout:  time.Second,
		MaxTimeout:  5 * time.Minute,
	}
}

// Config is read from the environment, the way the other services are configured.
type Config struct {
	Endpoint string
	// PluginsDir holds one directory per plugin. Empty means no plugins.
	PluginsDir string
	Limits     Limits
}

// ConfigFromEnv reads FEEDS_SERVICE_ENDPOINT and FEEDS_PLUGINS_DIR. getenv is os.Getenv in
// production and a map lookup in tests.
func ConfigFromEnv(getenv func(string) string) Config {
	endpoint := strings.TrimSpace(getenv("FEEDS_SERVICE_ENDPOINT"))
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return Config{
		Endpoint:   endpoint,
		PluginsDir: strings.TrimSpace(getenv("FEEDS_PLUGINS_DIR")),
		Limits:     DefaultLimits(),
	}
}

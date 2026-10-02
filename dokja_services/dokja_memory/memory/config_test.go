package memory

import (
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestConfigFromEnvDefaults(t *testing.T) {
	config, err := ConfigFromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "tcp://*:5562" || config.DBFile != "dokja_memory.db" || !config.EmbedEnabled ||
		config.EmbedEndpoint != "http://127.0.0.1:11434" || config.EmbedModel != "bge-m3" ||
		config.EmbedTimeout != 5*time.Second || config.ExpireAfter != 30*24*time.Hour {
		t.Fatalf("defaults: %+v", config)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	config, err := ConfigFromEnv(env(map[string]string{
		"MEMORY_SERVICE_ENDPOINT": "tcp://*:6000", "MEMORY_DB_FILE": "/data/m.db", "DOKJA_EMBED": "off",
		"DOKJA_EMBED_ENDPOINT": "http://embedder:11434", "DOKJA_EMBED_MODEL": "other", "DOKJA_EMBED_TIMEOUT": "2.5",
		"MEMORY_EXPIRE_AFTER": "48h",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "tcp://*:6000" || config.DBFile != "/data/m.db" || config.EmbedEnabled ||
		config.EmbedEndpoint != "http://embedder:11434" || config.EmbedModel != "other" ||
		config.EmbedTimeout != 2500*time.Millisecond || config.ExpireAfter != 48*time.Hour {
		t.Fatalf("overrides: %+v", config)
	}
	if never, _ := ConfigFromEnv(env(map[string]string{"MEMORY_EXPIRE_AFTER": "0"})); never.ExpireAfter != 0 {
		t.Fatal("0 must mean never expire")
	}
}

func TestConfigFromEnvRejectsBadValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"timeout text":     {"DOKJA_EMBED_TIMEOUT": "soon"},
		"timeout negative": {"DOKJA_EMBED_TIMEOUT": "-1"},
		"expiry text":      {"MEMORY_EXPIRE_AFTER": "a month"},
		"expiry negative":  {"MEMORY_EXPIRE_AFTER": "-5h"},
	} {
		if _, err := ConfigFromEnv(env(values)); err == nil {
			t.Fatalf("%s should be refused", name)
		}
	}
}

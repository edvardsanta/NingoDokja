package feeds

import "testing"

func TestConfigFromEnvDefaults(t *testing.T) {
	config := ConfigFromEnv(func(string) string { return "" })
	if config.Endpoint != DefaultEndpoint || config.PluginsDir != "" {
		t.Fatalf("%+v", config)
	}
	if config.Limits != DefaultLimits() {
		t.Fatalf("limits = %+v", config.Limits)
	}
}

func TestConfigFromEnvReadsTheVariables(t *testing.T) {
	env := map[string]string{"FEEDS_SERVICE_ENDPOINT": " tcp://*:6000 ", "FEEDS_PLUGINS_DIR": " /plugins "}
	config := ConfigFromEnv(func(name string) string { return env[name] })
	if config.Endpoint != "tcp://*:6000" || config.PluginsDir != "/plugins" {
		t.Fatalf("%+v", config)
	}
}

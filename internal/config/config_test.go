package config

import (
	"os"
	"path/filepath"
	"testing"

	"quant/internal/strategy"

	"gopkg.in/yaml.v3"
)

func TestDefaultAndExampleStrategiesMatchRegistry(t *testing.T) {
	registryNames := strategy.DefaultRegistry().List()
	dailyNames := strategy.DailyStrategyNames(registryNames)
	assertSameStrategySet(t, defaultConfig.Signal.DefaultStrategies, dailyNames, "default config")

	contents, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var example Config
	if err := yaml.Unmarshal(contents, &example); err != nil {
		t.Fatal(err)
	}
	assertSameStrategySet(t, example.Signal.DefaultStrategies, dailyNames, "example config")
}

func TestAIConfigurationSupportsCustomKeyEnvironment(t *testing.T) {
	t.Setenv("CUSTOM_DEEPSEEK_KEY", "secret")
	t.Setenv("QUANT_AI_BASE_URL", "https://gateway.example/v1")
	t.Setenv("QUANT_AI_MODEL", "deepseek-reasoner")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ai:\n  enabled: true\n  api_key_env: CUSTOM_DEEPSEEK_KEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.APIKey != "secret" || cfg.AI.BaseURL != "https://gateway.example/v1" || cfg.AI.Model != "deepseek-reasoner" {
		t.Fatalf("unexpected AI config: %+v", cfg.AI)
	}
}

func assertSameStrategySet(t *testing.T, actual, expected []string, source string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("%s has %d strategies, registry has %d", source, len(actual), len(expected))
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	for _, name := range actual {
		if !expectedSet[name] {
			t.Fatalf("%s contains unregistered strategy %q", source, name)
		}
		delete(expectedSet, name)
	}
	if len(expectedSet) > 0 {
		t.Fatalf("%s misses registered strategies %v", source, expectedSet)
	}
}

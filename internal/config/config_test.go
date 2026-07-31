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

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalAIFeatureDefaultsAndLegacyChoices(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Swarm.AutoTitle || !cfg.Swarm.Graph || cfg.Swarm.ComposerPredictions ||
			cfg.Swarm.GraphAutoRecord || cfg.Swarm.SelectionTranslation ||
			!cfg.Speech.Input.GatewayDisabled || !cfg.Speech.Output.GatewayDisabled ||
			cfg.Speech.Input.Enabled || cfg.Speech.Output.Enabled {
			t.Fatalf("fresh defaults swarm=%+v speech=%+v", cfg.Swarm, cfg.Speech)
		}
	}

	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, FileName), []byte(`swarm:
  auto_title: false
  composer_predictions: false
speech:
  input:
    gateway_disabled: true
`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Swarm.AutoTitle || legacy.Swarm.ComposerPredictions || !legacy.Swarm.GraphAutoRecord ||
		!legacy.Swarm.SelectionTranslation || !legacy.Speech.Input.GatewayDisabled || legacy.Speech.Output.GatewayDisabled {
		t.Fatalf("legacy choices swarm=%+v speech=%+v", legacy.Swarm, legacy.Speech)
	}
}

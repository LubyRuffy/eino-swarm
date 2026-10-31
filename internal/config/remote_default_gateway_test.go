package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteDefaultGatewayPreservesAuthenticationAndCustomOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, origin, mode string
		enabled                  bool
	}{
		{"first run", "", "https://ai.fofa.info:2440", RemoteAuthLegacy, false},
		{"legacy blank", "remote:\n  enabled: false\n  hub_url: ''\n", "https://ai.fofa.info:2440", RemoteAuthLegacy, false},
		{"account blank", "remote:\n  enabled: true\n  auth_mode: nosec\n  hub_url: '  '\n", "https://ai.fofa.info:2440", RemoteAuthNOSEC, true},
		{"custom anonymous", "remote:\n  enabled: true\n  auth_mode: legacy\n  hub_url: https://relay.example\n", "https://relay.example", RemoteAuthLegacy, true},
		{"custom account", "remote:\n  enabled: true\n  auth_mode: nosec\n  hub_url: https://gateway.example:2440\n", "https://gateway.example:2440", RemoteAuthNOSEC, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.yaml != "" {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Remote.HubURL != tc.origin || cfg.Remote.AuthMode != tc.mode || cfg.Remote.Enabled != tc.enabled {
				t.Fatalf("unexpected gateway defaults: %+v", cfg.Remote)
			}
			if cfg.HasHostToken() {
				t.Fatal("default configuration must not mint or activate a credential")
			}
			again, err := Load(dir)
			if err != nil || again.Remote.HubURL != tc.origin || again.Remote.AuthMode != tc.mode {
				t.Fatalf("gateway changed after reload: %+v, %v", again, err)
			}
		})
	}
}

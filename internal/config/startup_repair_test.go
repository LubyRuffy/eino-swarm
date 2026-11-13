package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnrecognizedSettingsAreClearedAndTheRestIsReloaded(t *testing.T) {
	dir := t.TempDir()
	const original = `models:
  default_source: neither
  account_presentation: poster
  providers:
    - id: kept
      api_key: secret-value
      model: named
      api_mode: responses
      builtin_tools:
        - type: crystal-ball
        - type: web_search
    - id: oldproto
      api_mode: soap
remote:
  auth_mode: legacy
  enabled: true
  hub_url: https://gateway.example
  display_name: desk
product:
  mode: kiosk
tools:
  web_execution: somewhere
`
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote.AuthMode != RemoteAuthAccount || !cfg.Remote.Enabled || cfg.Remote.HubURL != "https://gateway.example" || cfg.Remote.DisplayName != "desk" {
		t.Fatalf("recognized remote settings changed: %+v", cfg.Remote)
	}
	provider, ok := cfg.Provider("kept")
	if !ok || provider.APIKey != "secret-value" || provider.Model != "named" || provider.APIMode != APIModeResponses {
		t.Fatalf("provider was not kept: %+v %v", provider, ok)
	}
	old, ok := cfg.Provider("oldproto")
	if !ok || old.APIMode != "" {
		t.Fatalf("unrecognized protocol was kept: %+v %v", old, ok)
	}
	if len(provider.BuiltinTools) != 1 || provider.BuiltinTools[0]["type"] != "web_search" {
		t.Fatalf("recognized tool was dropped: %+v", provider.BuiltinTools)
	}
	if cfg.Product.Mode != "" || cfg.Tools.WebExecution != WebExecutionAuto || cfg.Models.DefaultSource == "neither" || cfg.Models.AccountPresentation != "" {
		t.Fatalf("defaults were not reloaded: product=%q web=%q source=%q presentation=%q", cfg.Product.Mode, cfg.Tools.WebExecution, cfg.Models.DefaultSource, cfg.Models.AccountPresentation)
	}
	repairs := cfg.StartupRepairs()
	want := []string{"remote.auth_mode", "product.mode", "tools.web_execution", "models.default_source", "models.account_presentation", "models.providers.kept.builtin_tools", "models.providers.oldproto.api_mode"}
	if len(repairs) != len(want) {
		t.Fatalf("repairs = %+v", repairs)
	}
	for i, setting := range want {
		if repairs[i].Setting != setting || repairs[i].Removed == "" && setting != "models.providers.kept.builtin_tools" {
			t.Fatalf("repair %d = %+v", i, repairs[i])
		}
	}
	if repairs[0].Removed != "legacy" || repairs[0].Current != RemoteAuthAccount {
		t.Fatalf("auth repair = %+v", repairs[0])
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "legacy") || strings.Contains(text, "secret-value") == false || !strings.Contains(text, "auth_mode: "+RemoteAuthAccount) {
		t.Fatalf("rewritten file did not clear only the unrecognized values:\n%s", text)
	}
	again, err := Load(dir)
	if err != nil || len(again.StartupRepairs()) != 0 || again.Remote.AuthMode != RemoteAuthAccount || again.Remote.DisplayName != "desk" {
		t.Fatalf("reload repeated the repair: %+v %+v", again.StartupRepairs(), again.Remote)
	}
}

func TestRepairValueClippingAndAToolThatCannotBeKept(t *testing.T) {
	got := clipRepairValue("bad\nvalue\x00" + strings.Repeat("长", 90))
	if strings.Contains(got, "\n") || strings.Contains(got, "\x00") || len([]rune(got)) != 80 {
		t.Fatalf("clipped value = %q (%d)", got, len([]rune(got)))
	}
	only := Provider{BuiltinTools: []map[string]any{{"type": "crystal-ball"}, {}}}
	if removed := dropUnrecognizedBuiltinTools(&only); len(removed) != 2 || only.BuiltinTools != nil {
		t.Fatalf("unknown tools stayed: %+v %v", only.BuiltinTools, removed)
	}
	known := Provider{BuiltinTools: []map[string]any{{"type": "web_search"}}}
	if dropUnrecognizedBuiltinTools(&known) != nil || len(known.BuiltinTools) != 1 {
		t.Fatal("recognized tool was cleared")
	}
}

func TestBlankAuthenticationDefaultsToAccountWithoutRewritingTheFile(t *testing.T) {
	dir := t.TempDir()
	const original = "remote:\n  enabled: false\n  hub_url: https://relay.example\n"
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote.AuthMode != RemoteAuthAccount || len(cfg.StartupRepairs()) != 0 || cfg.Remote.HubURL != "https://relay.example" {
		t.Fatalf("blank mode = %+v repairs=%+v", cfg.Remote, cfg.StartupRepairs())
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("blank mode was rewritten:\n%s", after)
	}
}

func TestExplicitIndependentAuthenticationIsKept(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("remote:\n  enabled: true\n  auth_mode: independent\n  hub_url: https://relay.example\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil || cfg.Remote.AuthMode != RemoteAuthIndependent || len(cfg.StartupRepairs()) != 0 {
		t.Fatalf("independent mode = %+v err=%v", cfg.Remote, err)
	}
}

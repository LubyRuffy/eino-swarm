package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func clearModelEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
}

// A fresh install has no model service of its own; the signed-in account is
// the path it is expected to use.
func TestFreshInstallPrefersTheAccountModelSource(t *testing.T) {
	clearModelEnv(t)
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.DefaultSource != DefaultSourceAccount {
		t.Fatalf("default source = %q, want account", cfg.Models.DefaultSource)
	}
	if cfg.UI.WelcomeDismissed {
		t.Fatal("the welcome gate starts dismissed")
	}
}

// A file written before the key existed must keep its behaviour: someone who
// already configured a working provider keeps using it after the upgrade.
func TestExistingReadyProviderKeepsItsOwnModelsAfterUpgrade(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: own\n  providers:\n    - id: own\n      base_url: http://127.0.0.1:1/v1\n      model: some-model\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.DefaultSource != DefaultSourceProvider {
		t.Fatalf("an upgraded ready provider lost its priority: %q", cfg.Models.DefaultSource)
	}
}

// An unconfigured file has nothing of its own to protect, so it follows the
// fresh-install default instead of staying on a blank provider.
func TestExistingBlankProviderFollowsTheAccountDefault(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: default\n  providers:\n    - id: default\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.DefaultSource != DefaultSourceAccount {
		t.Fatalf("blank provider config = %q, want account", cfg.Models.DefaultSource)
	}
}

// OPENAI_* variables are an explicit choice of a model service on first run.
func TestEnvironmentSeededProviderIsNotOverriddenByTheAccount(t *testing.T) {
	clearModelEnv(t)
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1/v1")
	t.Setenv("OPENAI_MODEL", "env-model")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.DefaultSource != DefaultSourceProvider {
		t.Fatalf("env-configured provider = %q, want provider", cfg.Models.DefaultSource)
	}
}

func TestExplicitDefaultSourceChoiceSurvivesReload(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: own\n  default_source: account\n  providers:\n    - id: own\n      base_url: http://127.0.0.1:1/v1\n      model: some-model\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.DefaultSource != DefaultSourceAccount {
		t.Fatalf("an explicit account choice was rewritten: %q", cfg.Models.DefaultSource)
	}
	if err := cfg.Update(func(next *Config) error { next.Models.DefaultSource = " Provider "; return nil }); err != nil {
		t.Fatal(err)
	}
	restored, err := Load(dir)
	if err != nil || restored.Models.DefaultSource != DefaultSourceProvider {
		t.Fatalf("saved choice did not survive reload: %q, %v", restored.Models.DefaultSource, err)
	}
}

func TestAbsentPresentationMeansOfficialAndIsNotWrittenBack(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: own\n  providers:\n    - id: own\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.AccountPresentation != "" || cfg.Models.Presentation() != AccountPresentationOfficial {
		t.Fatalf("absent presentation = %q (%s)", cfg.Models.AccountPresentation, cfg.Models.Presentation())
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil || strings.Contains(string(raw), "account_presentation") {
		t.Fatalf("loading an old file wrote the presentation: %s %v", raw, err)
	}
}

func TestFullPresentationSurvivesReload(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: own\n  account_presentation: full\n  providers:\n    - id: own\n")
	cfg, err := Load(dir)
	if err != nil || cfg.Models.Presentation() != AccountPresentationFull {
		t.Fatalf("full presentation = %q, %v", cfg.Models.AccountPresentation, err)
	}
}

func TestUnknownPresentationIsRejected(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  account_presentation: somewhere\n")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "account_presentation") {
		t.Fatalf("unknown presentation accepted: %v", err)
	}
	if (ModelsConfig{AccountPresentation: "somewhere"}).Presentation() != AccountPresentationOfficial {
		t.Fatal("an unreadable switch was not treated as official")
	}
}

func TestUnknownDefaultSourceIsRejectedRatherThanGuessed(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default_source: gateway\n")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "default_source") {
		t.Fatalf("unknown default source accepted: %v", err)
	}
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(next *Config) error { next.Models.DefaultSource = "other"; return nil }); err == nil {
		t.Fatal("a live save accepted an unknown default source")
	}
}

// Account sources are runtime projections addressed by a reserved id prefix.
// A hand-written provider using it could shadow or impersonate one.
func TestProviderIDsCannotUseTheAccountSourcePrefix(t *testing.T) {
	clearModelEnv(t)
	dir := writeConfigFile(t, "models:\n  default: gateway:abc\n  providers:\n    - id: gateway:abc\n      base_url: http://127.0.0.1:1/v1\n      model: m\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("a configured provider claimed the reserved account id prefix")
	}
}

func TestWelcomeDismissalIsPersistedAndMergedExplicitly(t *testing.T) {
	clearModelEnv(t)
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(next *Config) error { next.UI.WelcomeDismissed = true; return nil }); err != nil {
		t.Fatal(err)
	}
	restored, err := Load(cfg.DataDir())
	if err != nil || !restored.UI.WelcomeDismissed {
		t.Fatalf("dismissal lost across restart: %v, %v", restored.UI, err)
	}
	// A language-only patch must not bring the welcome gate back.
	merged := MergeUI(restored.UI, UIConfig{Locale: LocaleEn})
	if !merged.WelcomeDismissed || merged.Locale != LocaleEn {
		t.Fatalf("a partial UI patch reset the dismissal: %+v", merged)
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderHelpers(t *testing.T) {
	p := Provider{ID: "x"}
	if p.Ready() {
		t.Fatal("a provider with no base url is not ready")
	}
	if p.DisplayName() != "x" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	p.Model = "m"
	if p.DisplayName() != "m" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	p.Label = "Nice Name"
	if p.DisplayName() != "Nice Name" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	p.Label = ""
	if p.GroupName() != "x" {
		t.Fatalf("an unnamed provider groups as its id, not the model, got %q", p.GroupName())
	}
	p.Label = "Nice Name"
	if p.GroupName() != "Nice Name" {
		t.Fatalf("GroupName=%q", p.GroupName())
	}
	if p.Timeout() != DefaultRequestTimeout {
		t.Fatalf("Timeout=%v", p.Timeout())
	}
	p.BaseURL = "http://x.invalid/v1"
	if !p.Ready() {
		t.Fatal("base url + model is ready even without an api key")
	}

	cfg := Default()
	if _, ok := cfg.Provider("nope"); ok {
		t.Fatal("unknown provider must not resolve")
	}
	if _, ok := cfg.Provider(""); !ok {
		t.Fatal("empty id must resolve to the default provider")
	}

	if p.EndpointReady() && !p.Ready() {
		t.Fatal("a default model is still required to run a turn")
	}
	if !p.Listed() {
		t.Fatal("a provider with no switch must stay in the composer")
	}
}

// Turning an endpoint off hides it from the composer. An older file has no
// key, and writing enabled: true must not grow one — both mean on. Turning
// every row off is repaired so a new conversation still has a start.
func TestProviderListedSurvivesASaveAndAnAllOffFile(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("ZWAI_MODEL_BASE_URL", "")
	t.Setenv("ZWAI_MODEL_API_KEY", "")
	t.Setenv("ZWAI_MODEL_NAME", "")
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = append(cfg.Models.Providers, Provider{
		ID: "debug", Label: "Debug", BaseURL: "http://debug.invalid/v1", Model: "probe",
	})
	on := true
	cfg.Models.Providers[1].Enabled = &on
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	turnedOn, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !turnedOn.Models.Providers[1].Listed() || turnedOn.Models.Providers[1].Enabled != nil {
		t.Fatal("on must round-trip as the omitted default")
	}

	off := false
	turnedOn.Models.Providers[1].Enabled = &off
	if err := turnedOn.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Models.Providers[0].Listed() || reloaded.Models.Providers[0].Enabled != nil {
		t.Fatal("the endpoint that stays on grew a switch")
	}
	if reloaded.Models.Providers[1].Listed() || reloaded.Models.Providers[1].Enabled == nil || *reloaded.Models.Providers[1].Enabled {
		t.Fatal("the hidden endpoint did not stay off")
	}
	if _, ok := reloaded.Provider("debug"); !ok {
		t.Fatal("hiding an endpoint must not delete it")
	}
	reloaded.Models.Default = "debug"
	if err := reloaded.Save(); err != nil {
		t.Fatal(err)
	}
	moved, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	keptDefault, ok := moved.Provider(moved.Models.Default)
	if !ok || !keptDefault.Listed() || moved.Models.Default == "debug" {
		t.Fatalf("a hidden default must move to an endpoint the composer still lists, got %q", moved.Models.Default)
	}

	reloaded.Models.Providers[0].Enabled = &off
	if err := reloaded.Save(); err != nil {
		t.Fatal(err)
	}
	repaired, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept, ok := repaired.DefaultProvider()
	if !ok || !kept.Listed() {
		t.Fatal("turning every endpoint off must leave the default in the composer")
	}
}

// A provider is one endpoint, not one model: the catalog is what the composer
// lists, and the default is just the name new conversations start on.
func TestProviderCatalogDedupesAndIncludesTheDefault(t *testing.T) {
	p := Provider{
		Model:   "alpha",
		Catalog: []string{" beta ", "alpha", "", "gamma", "beta"},
	}
	got := p.Models()
	want := []string{"beta", "alpha", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Models=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Models=%v want %v", got, want)
		}
	}

	blank := Provider{}
	if names := blank.Models(); len(names) != 0 {
		t.Fatalf("an empty provider listed models: %v", names)
	}

	dir := t.TempDir()
	raw := strings.Join([]string{
		"models:",
		"  providers:",
		"    - id: default",
		"      catalog:",
		"        - one",
		"        - one",
		"        - two",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Models.Providers[0].Catalog; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("catalog not cleaned on load: %v", got)
	}
	if cfg.Models.Providers[0].Catalog == nil {
		t.Fatal("catalog must be a list, not null")
	}
}

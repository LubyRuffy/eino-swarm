package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAbsentProductModeIsFOFAAndOmitted(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Product.Resolved() != ProductModeFOFA || !cfg.Product.FOFA() {
		t.Fatalf("empty mode = %+v", cfg.Product)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "product:") {
		t.Fatalf("a new file wrote the default mode:\n%s", raw)
	}
}

func TestLocalProductModeRoundTrips(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Product.Mode = "LOCAL"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "mode: local") {
		t.Fatalf("local was not stored:\n%s", raw)
	}
	again, err := Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	if again.Product.Resolved() != ProductModeLocal || again.Product.FOFA() {
		t.Fatalf("reloaded mode = %+v", again.Product)
	}
}

func TestExplicitFOFAIsOmittedOnSave(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Product.Mode = "fofa"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "product:") {
		t.Fatalf("explicit fofa was written:\n%s", raw)
	}
	if cfg.Product.Mode != "" || !cfg.Product.FOFA() {
		t.Fatalf("stored mode = %+v", cfg.Product)
	}
}

func TestUnknownProductModeIsRejected(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Product.Mode = "cloud"
	if err := cfg.Save(); err == nil || !strings.Contains(err.Error(), "product.mode") {
		t.Fatalf("save err = %v", err)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cloud") {
		t.Fatalf("rejected mode was written:\n%s", raw)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("product:\n  mode: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a hand-edited unknown mode loaded")
	}
}

func TestSameModelSettingsTreatsEmptyAsEqual(t *testing.T) {
	base := ModelsConfig{Providers: []Provider{{ID: "a", Catalog: nil, BuiltinTools: nil}}}
	echo := ModelsConfig{Providers: []Provider{{ID: "a", Catalog: []string{}, BuiltinTools: []map[string]any{}}}}
	if !SameModelSettings(base, echo) {
		t.Fatal("nil and empty model lists were treated as a change")
	}
	on := true
	listed := echo
	listed.Providers = []Provider{{ID: "a", Enabled: &on}}
	if !SameModelSettings(base, listed) {
		t.Fatal("a missing enabled flag and an explicit listed flag were treated as a change")
	}
	moved := echo
	moved.Providers[0].BaseURL = "http://other.example/v1"
	if SameModelSettings(base, moved) {
		t.Fatal("a different endpoint was treated as the same model settings")
	}
}

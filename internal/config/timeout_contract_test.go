package config

import (
	"errors"
	"testing"
)

// A phone must show the same independent header budget the PC adapter uses.
func TestTimeoutPhoneViewKeepsFirstByteWaitLongerThanStreamIdle(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idle, first := 2, 120
	if err := cfg.Update(func(next *Config) error {
		return ApplyTimeoutPatch(next, TimeoutPatch{Providers: []ProviderTimeoutPatch{{ID: "default", TimeoutSeconds: &idle, FirstByteTimeoutSeconds: &first}}})
	}); err != nil {
		t.Fatal(err)
	}
	got := cfg.Timeouts().Providers[0]
	if got.FirstByteTimeoutSeconds != first || got.EffectiveFirstByteTimeoutSeconds != first || got.TimeoutSeconds != idle {
		t.Fatalf("phone view capped independent header wait: %+v", got)
	}
}

func TestTimeoutSnapshotsAndAbortedUpdatesDetachNestedCloudTools(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers[0].APIMode = APIModeResponses
	cfg.Models.Providers[0].BuiltinTools = []map[string]any{{"type": "code_interpreter", "container": map[string]any{"type": "auto", "file_ids": []string{"file-a"}, "precision": int64(9007199254740993)}}}
	check := func() {
		t.Helper()
		p, _ := cfg.Provider("")
		c := p.BuiltinTools[0]["container"].(map[string]any)
		if c["type"] != "auto" || c["file_ids"].([]string)[0] != "file-a" || c["precision"] != int64(9007199254740993) {
			t.Fatalf("cloud configuration changed: %+v", c)
		}
	}
	for _, snapshot := range []*Config{cfg.Snapshot(), {Models: cfg.ModelsSnapshot()}} {
		c := snapshot.Models.Providers[0].BuiltinTools[0]["container"].(map[string]any)
		c["type"] = "modified"
		c["file_ids"].([]string)[0] = "modified"
		check()
	}
	aborted := errors.New("aborted")
	if err := cfg.Update(func(next *Config) error {
		next.Models.Providers[0].BuiltinTools[0]["container"].(map[string]any)["type"] = "unsaved"
		return aborted
	}); !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	check()
	if err := cfg.Update(func(next *Config) error { next.Models.Providers[0].BuiltinTools[0]["type"] = "unsupported"; return nil }); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("provider validation disappeared: %v", err)
	}
	check()
	first := 90
	if err := cfg.Update(func(next *Config) error {
		return ApplyTimeoutPatch(next, TimeoutPatch{Providers: []ProviderTimeoutPatch{{ID: "default", FirstByteTimeoutSeconds: &first}}})
	}); err != nil {
		t.Fatal(err)
	}
	check()
}

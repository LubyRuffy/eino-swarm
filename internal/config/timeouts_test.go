package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestTimeoutSnapshotAndUpdateKeepDetachedStateAndRollback(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.Models.Providers[0].Enabled = &off
	cfg.Models.Providers[0].ModelContext = map[string]int{"a": 4}
	cfg.Models.Providers[0].Catalog = []string{"a"}
	s := cfg.Snapshot()
	s.Models.Providers[0].Catalog[0] = "b"
	s.Models.Providers[0].ModelContext["a"] = 8
	*s.Models.Providers[0].Enabled = true
	if cfg.Models.Providers[0].Catalog[0] != "a" || cfg.Models.Providers[0].ModelContext["a"] != 4 || *cfg.Models.Providers[0].Enabled {
		t.Fatal("snapshot aliases live mutable state")
	}
	wantErr := errors.New("validation")
	if err := cfg.Update(func(c *Config) error { c.Swarm.AgentTimeoutSeconds = 7; return wantErr }); !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	if cfg.Swarm.AgentTimeoutSeconds == 7 {
		t.Fatal("aborted candidate published")
	}
	if cfg.Update(nil) == nil {
		t.Fatal("nil edit accepted")
	}
	var literal Config
	if literal.Snapshot() == nil || literal.ModelsSnapshot().Default != "" {
		t.Fatal("literal snapshot")
	}
}

func TestTimeoutConcurrentPatchesMergeWithTheLatestDesktopSettings(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := cfg.Update(func(c *Config) error { c.UI.Locale = "zh"; return nil }); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			n := 41
			if err := cfg.Update(func(c *Config) error { return ApplyTimeoutPatch(c, TimeoutPatch{AgentTimeoutSeconds: &n}) }); err != nil {
				t.Error(err)
			}
			_ = cfg.Timeouts()
			_, _ = cfg.Provider("")
		}()
	}
	wg.Wait()
	s := cfg.Snapshot()
	if s.UI.Locale != "zh" || s.Swarm.AgentTimeoutSeconds != 41 {
		t.Fatal("concurrent save lost unrelated settings")
	}
	disk, err := Load(cfg.DataDir())
	if err != nil || !reflect.DeepEqual(disk.Swarm, s.Swarm) || disk.UI.Locale != "zh" {
		t.Fatal("disk/live mismatch")
	}
}

func TestTimeoutTypedPatchValidatesEveryEditedFieldBeforePublishing(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ptr := func(n int) *int { return &n }
	for _, p := range []TimeoutPatch{
		{}, {AgentTimeoutSeconds: ptr(0)}, {AgentTimeoutSeconds: ptr(9223372037)},
		{Providers: []ProviderTimeoutPatch{{ID: "missing", TimeoutSeconds: ptr(1)}}},
		{Providers: []ProviderTimeoutPatch{{ID: ""}}}, {Providers: []ProviderTimeoutPatch{{ID: "default"}}},
		{Providers: []ProviderTimeoutPatch{{ID: "default", TimeoutSeconds: ptr(-1)}}},
		{Providers: []ProviderTimeoutPatch{{ID: "default", FirstByteTimeoutSeconds: ptr(0)}}},
		{Providers: []ProviderTimeoutPatch{{ID: "default", TimeoutSeconds: ptr(1)}, {ID: "default", TimeoutSeconds: ptr(2)}}},
	} {
		before := cfg.Timeouts()
		if err := cfg.Update(func(c *Config) error { return ApplyTimeoutPatch(c, p) }); !errors.Is(err, ErrTimeoutSettings) {
			t.Fatalf("invalid patch accepted: %+v", p)
		}
		if !reflect.DeepEqual(before, cfg.Timeouts()) {
			t.Fatal("invalid patch partially applied")
		}
	}
	if err := cfg.Update(func(c *Config) error {
		return ApplyTimeoutPatch(c, TimeoutPatch{AgentTimeoutSeconds: ptr(66), Providers: []ProviderTimeoutPatch{{ID: "default", TimeoutSeconds: ptr(6), FirstByteTimeoutSeconds: ptr(9)}}})
	}); err != nil {
		t.Fatal(err)
	}
	v := cfg.Timeouts()
	if v.AgentTimeoutSeconds != 66 || v.Providers[0].TimeoutSeconds != 6 || v.Providers[0].EffectiveFirstByteTimeoutSeconds != 9 {
		t.Fatal(v)
	}
	if cfg.SwarmSnapshot().AgentTimeoutSeconds != 66 {
		t.Fatal("swarm snapshot stale")
	}
	for _, value := range []int{0, -1, 11} {
		p := Provider{FirstByteTimeoutSeconds: value}
		want := 30 * time.Second
		if value > 0 {
			want = 11 * time.Second
		}
		if p.FirstByteTimeout() != want {
			t.Fatal("default/explicit first-byte")
		}
	}
	if err := cfg.Update(func(c *Config) error {
		c.Log.Level = "debug"
		c.Remote.ThreadLimit = 9
		c.Search.Embedding = true
		c.Clients.RecentDays = 8
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	disk, err := Load(cfg.DataDir())
	if err != nil || disk.Log.Level != "debug" || disk.Remote.ThreadLimit != 9 || !disk.Search.Embedding || disk.Clients.RecentDays != 8 {
		t.Fatal("shared save lost other sections")
	}
}

func TestTimeoutDefaultsAllowAnHourWithoutOverwritingExplicitValues(t *testing.T) {
	if got := Default().Swarm.AgentTimeout(); got != time.Hour {
		t.Fatalf("new worker default=%s, want one hour", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("swarm:\n  agent_timeout_seconds: 600\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Swarm.AgentTimeout() != 10*time.Minute {
		t.Fatal("explicit old timeout was overwritten")
	}
}

func TestTimeoutSaveFailureDoesNotPublishUnsavedSettings(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := cfg.Swarm.AgentTimeoutSeconds
	// The destination cannot be atomically replaced, even as an elevated user.
	if err := os.Remove(cfg.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.Path(), 0700); err != nil {
		t.Fatal(err)
	}
	next := *cfg
	next.Swarm.AgentTimeoutSeconds = 41
	if err := cfg.Replace(&next); err == nil {
		t.Fatal("save must fail")
	}
	if cfg.Swarm.AgentTimeoutSeconds != old {
		t.Fatal("failed save changed the live timeout")
	}
}

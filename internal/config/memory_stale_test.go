package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSkillStaleDaysDefaultsRepairsAndRoundTrips(t *testing.T) {
	if got := Default().Memory.StaleDays(); got != DefaultSkillStaleDays {
		t.Fatalf("default stale days=%d", got)
	}
	for _, value := range []int{-1, 0, 7, 365} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("memory:\n  skill_stale_days: "+strconv.Itoa(value)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := value
			if want <= 0 {
				want = DefaultSkillStaleDays
			}
			if cfg.Memory.SkillStaleDays != want || cfg.Memory.StaleDays() != want || (MemoryConfig{SkillStaleDays: value}).StaleDays() != want {
				t.Fatalf("stale config=%+v want=%d", cfg.Memory, want)
			}
			raw, _ := json.Marshal(cfg.Memory)
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil || wire["skill_stale_days"] != float64(want) {
				t.Fatalf("wire=%s err=%v", raw, err)
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			again, err := Load(dir)
			if err != nil || again.Memory.SkillStaleDays != want {
				t.Fatalf("round trip=%+v err=%v", again, err)
			}
		})
	}
}

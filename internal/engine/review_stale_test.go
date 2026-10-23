package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

// Staleness is review input, never a deletion command, and the same candidates
// remain attributable to the original turn even after skill_view updates usage.
func TestTidyRecordsTheStaleCandidatesInItsOriginalTurn(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Memory.SkillStaleDays = 10
	p, th := projectThread(t, e)
	turn := plantDoneTurn(t, e, th.ID, time.Now().UTC())
	mem := e.ProjectMemory(p.ID)
	plantProjectSkill(t, e, p.ID, "old-method", "when maintaining dormant records", "Inspect the source and verify the retained identity.")
	path := filepath.Join(mem.Dir(), "skills", "old-method", "SKILL.md")
	old := time.Now().Add(-11 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	var scan []string
	report, err := e.FoldProjectSkillsWatch(p.ID, func(ev TidyEvent) {
		if ev.Phase == "scan" {
			scan = ev.StaleCandidates
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"old-method"}
	if !reflect.DeepEqual(report.StaleCandidates, want) || !reflect.DeepEqual(scan, want) {
		t.Fatalf("report=%+v scan=%v", report, scan)
	}
	if _, err := mem.ReadSkill("old-method"); err != nil {
		t.Fatal("stale candidate was deleted", err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			var out reviewOutcome
			if err := json.Unmarshal([]byte(ev.Text), &out); err != nil {
				t.Fatal(err)
			}
			if reflect.DeepEqual(out.StaleCandidates, want) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Trace lost stale candidates")
	}
	msg, err := catalogTidyUserMessage(mem, false, 10, []memory.SkillInfo{{Name: "old-method"}})
	if err != nil || !strings.Contains(msg, "old-method") || !strings.Contains(msg, "10 days") {
		t.Fatalf("input=%s err=%v", msg, err)
	}
	lib, err := catalogTidyUserMessage(mem, true, 10, nil)
	if err != nil || !strings.Contains(lib, "shared library") {
		t.Fatalf("library input=%s err=%v", lib, err)
	}
}

package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRequestedTidyMovesRecordedNotesThroughTheOfflineToolGraph(t *testing.T) {
	for _, action := range []string{"promote", "demote"} {
		t.Run(action, func(t *testing.T) {
			e := newTestEngine(t)
			p, th := projectThread(t, e)
			turn := plantDoneTurn(t, e, th.ID, time.Now().UTC())
			mem := e.ProjectMemory(p.ID)
			const procedure = "1. Inspect the durable inputs.\n2. Verify the resulting output."
			const fact = "The project retains its established source identity."
			if action == "promote" {
				if _, _, err := mem.Add(procedure); err != nil {
					t.Fatal(err)
				}
			} else {
				plantProjectSkill(t, e, p.ID, "recorded-fact", "When checking the established project constraint", fact)
			}
			var actions []string
			report, err := e.FoldProjectSkillsWatch(p.ID, func(ev TidyEvent) {
				if ev.Phase == "change" {
					actions = append(actions, ev.Action)
				}
			})
			if err != nil || report.Err != "" {
				t.Fatalf("tidy=%+v err=%v", report, err)
			}
			found := false
			for _, a := range actions {
				if a == action {
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s receipt through actual reviewer: %+v / %v", action, report, actions)
			}
			snap, err := mem.Read()
			if err != nil {
				t.Fatal(err)
			}
			skills, err := mem.ListSkills()
			if err != nil {
				t.Fatal(err)
			}
			if action == "promote" {
				if len(snap.Entries) != 0 || len(skills) != 1 {
					t.Fatalf("notes=%v skills=%v", snap.Entries, skills)
				}
				skill, err := mem.ReadSkill(skills[0].Name)
				if err != nil || skill.Body != procedure {
					t.Fatalf("source lost: %+v %v", skill, err)
				}
			} else if len(skills) != 0 || len(snap.Entries) != 1 || snap.Entries[0] != fact {
				t.Fatalf("notes=%v skills=%v", snap.Entries, skills)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			traced := false
			for _, ev := range events {
				if ev.Kind == KindMemoryReview {
					var out reviewOutcome
					if err := json.Unmarshal([]byte(ev.Text), &out); err != nil {
						t.Fatal(err)
					}
					for _, change := range out.Changes {
						if change.Action == action {
							traced = true
						}
					}
				}
			}
			if !traced {
				t.Fatalf("original turn trace lost %s", action)
			}
			again, err := e.FoldProjectSkills(p.ID)
			if err != nil || again.Err != "" {
				t.Fatalf("retry=%+v %v", again, err)
			}
			for _, c := range again.Changes {
				if c.Action == action {
					t.Fatalf("retry repeated move: %+v", again)
				}
			}
		})
	}
}

func TestTurnReviewCannotReceiveTheReclassificationTool(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Retain the explicit durable project requirement."}})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	var request struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(requests[0], &request); err != nil {
		t.Fatal(err)
	}
	for _, tool := range request.Tools {
		if tool.Function.Name == memory.ToolMemoryReclassify {
			t.Fatalf("review received move tool: %s", requests[0])
		}
	}
	if strings.Contains(string(requests[0]), `"promote"`) {
		t.Fatal("post-turn review advertised reclassification")
	}
}

func TestRequestedTidyKeepsPartialWritesAndReportsFailureInTrace(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	turn := plantDoneTurn(t, e, th.ID, time.Now().UTC())
	const fact = "The project preserves its established data ownership."
	mem := e.ProjectMemory(p.ID)
	plantProjectSkill(t, e, p.ID, "standing-fact", "When checking project ownership", fact)
	parent := filepath.Join(mem.Dir(), "skills")
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(parent, 0700); err != nil {
			t.Errorf("restore skill permissions: %v", err)
		}
	}()
	var added bool
	report, err := e.FoldProjectSkillsWatch(p.ID, func(ev TidyEvent) {
		if ev.Action == "add" {
			added = true
		}
	})
	if err != nil || report.Err == "" || !report.Folded() || report.Demoted != 0 || !added {
		t.Fatalf("partial report=%+v err=%v added=%v", report, err, added)
	}
	snap, err := mem.Read()
	if err != nil || len(snap.Entries) != 1 || snap.Entries[0] != fact {
		t.Fatalf("target lost: %+v %v", snap, err)
	}
	if _, err := mem.ReadSkill("standing-fact"); err != nil {
		deleted := false
		for _, c := range report.Changes {
			if c.Action == "delete" && c.Name == "standing-fact" {
				deleted = true
			}
		}
		if !deleted {
			t.Fatal("partial source deletion was omitted", err)
		}
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == KindMemoryReview && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("Trace omitted partial storage failure")
	}
}

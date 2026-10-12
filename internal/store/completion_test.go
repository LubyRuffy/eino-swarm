package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func completionThread(t *testing.T, s *Store) *Thread {
	t.Helper()
	th := &Thread{Title: "task"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	return th
}

func completionEvent(t *testing.T, s *Store, id, kind string) (*Turn, *Event) {
	t.Helper()
	turn := &Turn{ThreadID: id}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	ev := &Event{ThreadID: id, TurnID: turn.ID, Kind: kind, AgentID: "manager"}
	if err := s.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	return turn, ev
}

func assertCompletion(t *testing.T, s *Store, id string, completed, read int64) {
	t.Helper()
	th, err := s.GetThread(id)
	if err != nil {
		t.Fatal(err)
	}
	if th.CompletedSeq != completed || th.ReadCompletedSeq != read {
		t.Fatalf("completion=%d read=%d, want %d/%d", th.CompletedSeq, th.ReadCompletedSeq, completed, read)
	}
}

func TestCompletionPersistsBeforeFinishAndAcknowledgesOnlyObservedEvent(t *testing.T) {
	s := open(t)
	th := completionThread(t, s)
	turn, first := completionEvent(t, s, th.ID, "done")
	got, _ := s.GetTurn(turn.ID)
	if got.Status != TurnRunning || !first.Completion {
		t.Fatalf("event must precede FinishTurn: %+v %+v", got, first)
	}
	assertCompletion(t, s, th.ID, first.Seq, 0)
	if err := s.MarkThreadRead(th.ID, first.Seq); err != nil {
		t.Fatal(err)
	}
	_, second := completionEvent(t, s, th.ID, "error")
	if err := s.MarkThreadRead(th.ID, first.Seq); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, second.Seq, first.Seq)
	if err := s.MarkThreadRead(th.ID, second.Seq+1); !errors.Is(err, ErrInvalidCompletionSeq) {
		t.Fatalf("future read: %v", err)
	}
	if err := s.MarkThreadRead(th.ID, second.Seq); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, second.Seq, second.Seq)
	if err := s.MarkThreadRead("missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.MarkThreadRead(th.ID, 0); !errors.Is(err, ErrInvalidCompletionSeq) {
		t.Fatalf("zero: %v", err)
	}
	// Internal eligibility never changes the public replay payload.
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Completion") || strings.Contains(string(raw), "completion") {
		t.Fatalf("eligibility leaked: %s", raw)
	}
}

func TestWorkerCancelledQuietAndUnknownTurnDoNotCreateCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, status, kind, agent, role string
		quiet, suppress, missing        bool
	}{
		{name: "worker", kind: "done", agent: "worker"},
		{name: "worker role without agent", kind: "done", role: "worker"},
		{name: "progress", kind: "delta", agent: "manager"},
		{name: "cancelled", status: TurnCancelled, kind: "error", agent: "manager"},
		{name: "quiet", kind: "done", agent: "manager", quiet: true},
		{name: "runtime cancellation", kind: "error", agent: "manager", suppress: true},
		{name: "unknown turn", kind: "done", agent: "manager", missing: true},
		{name: "no turn", kind: "done", agent: "manager"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			th := completionThread(t, s)
			turn := &Turn{ThreadID: th.ID, Status: tc.status, Quiet: tc.quiet}
			if tc.name != "no turn" {
				if err := s.CreateTurn(turn); err != nil {
					t.Fatal(err)
				}
			}
			if tc.missing {
				turn.ID = "missing"
			}
			ev := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: tc.kind, AgentID: tc.agent, Role: tc.role, SuppressCompletion: tc.suppress}
			if err := s.AppendEvent(ev); err != nil {
				t.Fatal(err)
			}
			assertCompletion(t, s, th.ID, 0, 0)
			if primaryTerminal(ev) {
				if err := s.MarkThreadRead(th.ID, ev.Seq); err != nil {
					t.Fatalf("suppressed observation: %v", err)
				}
			} else {
				if err := s.MarkThreadRead(th.ID, ev.Seq); !errors.Is(err, ErrInvalidCompletionSeq) {
					t.Fatalf("non-terminal: %v", err)
				}
			}
		})
	}
}

func TestQuietCompletionRemovalKeepsOlderUnreadAndNewerCompletion(t *testing.T) {
	s := open(t)
	th := completionThread(t, s)
	_, first := completionEvent(t, s, th.ID, "done")
	quiet, q := completionEvent(t, s, th.ID, "done")
	if err := s.UpdateTurn(quiet.ID, map[string]any{"quiet": true}); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, first.Seq, 0)
	if err := s.MarkThreadRead(th.ID, q.Seq); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, first.Seq, 0)
	if err := s.MarkThreadRead(th.ID, first.Seq); err != nil {
		t.Fatal(err)
	}
	lateQuiet, _ := completionEvent(t, s, th.ID, "done")
	_, newer := completionEvent(t, s, th.ID, "error")
	if err := s.UpdateTurn(lateQuiet.ID, map[string]any{"quiet": true}); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, newer.Seq, first.Seq)
}

func TestCompletionReadSurvivesRestartAndRewindCannotHideNewCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := completionThread(t, s)
	_, first := completionEvent(t, s, th.ID, "done")
	_, second := completionEvent(t, s, th.ID, "error")
	if err := s.MarkThreadRead(th.ID, second.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, second.Seq, second.Seq)
	if err := s.TruncateFromEventSeq(th.ID, second.Seq); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, first.Seq, first.Seq)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_, next := completionEvent(t, s, th.ID, "done")
	assertCompletion(t, s, th.ID, next.Seq, first.Seq)
	if err := s.MarkThreadRead(th.ID, second.Seq); !errors.Is(err, ErrInvalidCompletionSeq) {
		t.Fatalf("late removed completion acknowledgment: %v", err)
	}
	assertCompletion(t, s, th.ID, next.Seq, first.Seq)
	if next.Seq <= second.Seq {
		t.Fatalf("restart reused a removed terminal identity: %d/%d", next.Seq, second.Seq)
	}
}

func TestCompletionMigrationDoesNotLightOldHistoryOrReviveItAfterQuiet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := completionThread(t, s)
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	// Old databases migrate these events with eligibility=false, not inferred
	// from a historical terminal kind or a changed timestamp.
	old := &Event{ThreadID: th.ID, TurnID: turn.ID, Seq: 1, Kind: "done", AgentID: "manager"}
	if err := s.DB().Create(old).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"completed_seq", "read_completed_seq", "terminal_seq_high_water"} {
		if err := s.DB().Migrator().DropColumn(&Thread{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB().Migrator().DropColumn(&Event{}, "completion"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	migrated, err := s.GetEvent(th.ID, old.Seq)
	if err != nil || migrated.Completion {
		t.Fatalf("legacy migration inferred completion: %+v %v", migrated, err)
	}
	assertCompletion(t, s, th.ID, 0, 0)
	legacyThread, err := s.GetThread(th.ID)
	if err != nil || legacyThread.TerminalSeqHighWater != 0 {
		t.Fatalf("migration backfilled terminal identity: %+v %v", legacyThread, err)
	}
	if err := s.MarkThreadRead(th.ID, old.Seq); err != nil {
		t.Fatal(err)
	}
	quiet, _ := completionEvent(t, s, th.ID, "done")
	if err := s.UpdateTurn(quiet.ID, map[string]any{"quiet": true}); err != nil {
		t.Fatal(err)
	}
	assertCompletion(t, s, th.ID, 0, 0)
}

func TestCompletionSuppressedIdentitySurvivesRemovalAndRestart(t *testing.T) {
	for _, suppress := range []bool{false, true} {
		t.Run(fmt.Sprint(suppress), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "suppressed.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			th := completionThread(t, s)
			turn := &Turn{ThreadID: th.ID, Quiet: !suppress}
			if err := s.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			ev := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done", SuppressCompletion: suppress}
			if err := s.AppendEvent(ev); err != nil {
				t.Fatal(err)
			}
			stored, err := s.GetThread(th.ID)
			if err != nil || stored.TerminalSeqHighWater != ev.Seq || stored.CompletedSeq != 0 {
				t.Fatalf("suppressed terminal lost identity: %+v %v", stored, err)
			}
			raw, err := json.Marshal(stored)
			if err != nil || strings.Contains(string(raw), "terminal_seq") {
				t.Fatalf("internal floor leaked: %s %v", raw, err)
			}
			if err := s.TruncateFromEventSeq(th.ID, ev.Seq); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			_, next := completionEvent(t, s, th.ID, "done")
			if next.Seq <= ev.Seq {
				t.Fatalf("suppressed terminal identity reused: %d/%d", next.Seq, ev.Seq)
			}
			if err := s.MarkThreadRead(th.ID, ev.Seq); !errors.Is(err, ErrInvalidCompletionSeq) {
				t.Fatalf("removed suppressed ACK: %v", err)
			}
			assertCompletion(t, s, th.ID, next.Seq, 0)
		})
	}
}

func TestCompletionSequenceSeedFailureAndFloorWriteAreNotPartial(t *testing.T) {
	t.Run("seed failure", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		if err := s.DB().Migrator().DropColumn(&Thread{}, "terminal_seq_high_water"); err != nil {
			t.Fatal(err)
		}
		ev := &Event{ThreadID: th.ID, Kind: "done"}
		if err := s.AppendEvent(ev); err == nil || !strings.Contains(err.Error(), "seed event sequence") {
			t.Fatalf("floor failure silently seeded zero: %v", err)
		}
		var n int64
		s.DB().Model(&Event{}).Count(&n)
		if n != 0 || ev.Seq != 0 {
			t.Fatalf("failed seed mutated event: %d/%d", n, ev.Seq)
		}
	})
	t.Run("floor write rollback", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		if err := s.DB().Exec(`CREATE TRIGGER reject_floor BEFORE UPDATE OF terminal_seq_high_water ON threads BEGIN SELECT RAISE(ABORT,'floor failure'); END`).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(&Event{ThreadID: th.ID, Kind: "error", SuppressCompletion: true}); err == nil {
			t.Fatal("suppressed terminal floor failure was hidden")
		}
		var n int64
		s.DB().Model(&Event{}).Count(&n)
		if n != 0 {
			t.Fatalf("failed floor left terminal row: %d", n)
		}
		assertCompletion(t, s, th.ID, 0, 0)
	})
}

func TestCompletionOldReadRacingNewTerminalCannotConsumeIt(t *testing.T) {
	s := open(t)
	th := completionThread(t, s)
	for i := 0; i < 20; i++ {
		_, first := completionEvent(t, s, th.ID, "done")
		turn := &Turn{ThreadID: th.ID}
		if err := s.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		next := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}
		var wg sync.WaitGroup
		wg.Add(2)
		errs := make(chan error, 2)
		go func() { defer wg.Done(); errs <- s.MarkThreadRead(th.ID, first.Seq) }()
		go func() { defer wg.Done(); errs <- s.AppendEvent(next) }()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		assertCompletion(t, s, th.ID, next.Seq, first.Seq)
	}
}

func TestCompletionTransactionFailuresDoNotPartiallyClearOrPublish(t *testing.T) {
	s := open(t)
	th := completionThread(t, s)
	turn, ev := completionEvent(t, s, th.ID, "done")
	if err := s.DB().Exec(`CREATE TRIGGER reject_completion BEFORE UPDATE OF completed_seq ON threads BEGIN SELECT RAISE(ABORT,'completion failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	later := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "error"}
	if err := s.AppendEvent(later); err == nil {
		t.Fatal("expected atomic completion failure")
	}
	var n int64
	s.DB().Model(&Event{}).Where("thread_id = ?", th.ID).Count(&n)
	if n != 1 {
		t.Fatalf("failed completion persisted event: %d", n)
	}
	if err := s.UpdateTurn(turn.ID, map[string]any{"quiet": true}); err == nil {
		t.Fatal("expected quiet rollback")
	}
	got, _ := s.GetTurn(turn.ID)
	if got.Quiet {
		t.Fatal("failed quiet update was partial")
	}
	stored, _ := s.GetEvent(th.ID, ev.Seq)
	if !stored.Completion {
		t.Fatal("failed quiet update cleared eligibility")
	}
	if err := s.DB().Exec(`DROP TRIGGER reject_completion`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec(`CREATE TRIGGER reject_read BEFORE UPDATE OF read_completed_seq ON threads BEGIN SELECT RAISE(ABORT,'read failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.MarkThreadRead(th.ID, ev.Seq); err == nil {
		t.Fatal("expected read rollback")
	}
	assertCompletion(t, s, th.ID, ev.Seq, 0)
}

func TestCompletionListingExcludesArchivedAndReadThreads(t *testing.T) {
	s := open(t)
	th := completionThread(t, s)
	_, ev := completionEvent(t, s, th.ID, "done")
	rows, err := s.ListUnreadThreads()
	if err != nil || len(rows) != 1 {
		t.Fatalf("unread list %+v %v", rows, err)
	}
	if err := s.UpdateThread(th.ID, map[string]any{"archived": true}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListUnreadThreads()
	if err != nil || len(rows) != 0 {
		t.Fatalf("archived list %+v %v", rows, err)
	}
	if err := s.UpdateThread(th.ID, map[string]any{"archived": false}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkThreadRead(th.ID, ev.Seq); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListUnreadThreads()
	if err != nil || len(rows) != 0 {
		t.Fatalf("read list %+v %v", rows, err)
	}
}

func TestCompletionDatabaseFailuresKeepErrorsAndState(t *testing.T) {
	t.Run("ordinary turn patch", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		turn, ev := completionEvent(t, s, th.ID, "error")
		if err := s.UpdateTurn(turn.ID, map[string]any{"quiet": false, "status": TurnError, "error": "request rejected"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetTurn(turn.ID)
		if err != nil || got.Status != TurnError || got.Error != "request rejected" || got.Quiet {
			t.Fatalf("ordinary patch became completion suppression: %+v %v", got, err)
		}
		assertCompletion(t, s, th.ID, ev.Seq, 0)
	})
	t.Run("empty turn patch", func(t *testing.T) {
		s := open(t)
		if err := s.UpdateTurn("missing", nil); err != nil {
			t.Fatalf("empty patch changed its existing no-op contract: %v", err)
		}
	})
	t.Run("event insert rollback", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		turn := &Turn{ThreadID: th.ID}
		if err := s.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		if err := s.DB().Exec(`CREATE TRIGGER reject_terminal BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}); err == nil {
			t.Fatal("terminal insertion failure was hidden")
		}
		assertCompletion(t, s, th.ID, 0, 0)
	})
	t.Run("reset missing events", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		_, ev := completionEvent(t, s, th.ID, "done")
		if err := s.DB().Migrator().DropTable(&Event{}); err != nil {
			t.Fatal(err)
		}
		if err := refreshCompletionWatermark(s.DB(), th.ID); err == nil {
			t.Fatal("watermark reset hid missing timeline")
		}
		assertCompletion(t, s, th.ID, ev.Seq, 0)
	})
	t.Run("closed", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkThreadRead(th.ID, 1); err == nil {
			t.Fatal("read hid database failure")
		}
		if _, err := s.ListUnreadThreads(); err == nil {
			t.Fatal("list hid database failure")
		}
		if err := s.UpdateTurn("missing", map[string]any{"quiet": true}); err == nil {
			t.Fatal("quiet hid database failure")
		}
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: "missing", Kind: "done"}); err == nil {
			t.Fatal("append hid database failure")
		}
	})
	t.Run("missing turn", func(t *testing.T) {
		s := open(t)
		if err := s.UpdateTurn("missing", map[string]any{"quiet": true}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing quiet: %v", err)
		}
	})
	t.Run("lost events", func(t *testing.T) {
		s := open(t)
		th := completionThread(t, s)
		turn, ev := completionEvent(t, s, th.ID, "done")
		if err := s.DB().Migrator().DropTable(&Event{}); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkThreadRead(th.ID, ev.Seq); err == nil {
			t.Fatal("read hid event database failure")
		}
		if err := s.UpdateTurn(turn.ID, map[string]any{"quiet": true}); err == nil {
			t.Fatal("quiet hid event database failure")
		}
	})
}

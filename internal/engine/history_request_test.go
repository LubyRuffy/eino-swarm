package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestExplicitHistoryReportRequestIsDurableAndKeepsItsTurnOwner(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("Explicit history request", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "Review the original records"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.recordHistoryReportRequest(turn); err != nil {
		t.Fatal(err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 || events[0].Kind != KindHistoryReportRequested || events[0].ThreadID != th.ID || events[0].Seq <= 0 {
		t.Fatalf("request mode was not durably owned: %+v %v", events, err)
	}
	// A late request callback cannot recreate metadata after its owner was dropped.
	e.recordMu.Lock()
	e.droppedTurns[turn.ID] = struct{}{}
	e.recordMu.Unlock()
	if err := e.recordHistoryReportRequest(turn); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dropped request owner was reused: %v", err)
	}
}

func TestHistoryReportRequestDoesNotHideAStorageRefusal(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("Failed history request", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "Review original records"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("history request write refused")
	if err := e.Store().DB().Callback().Create().Before("gorm:create").Register("history-request-write-failure", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindHistoryReportRequested {
			_ = tx.AddError(fault)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.recordHistoryReportRequest(turn); !errors.Is(err, fault) {
		t.Fatalf("storage cause was lost: %v", err)
	}
}

func TestExplicitHistoryReportCannotSilentlyBecomeSteering(t *testing.T) {
	e := newTestEngine(t)
	err := e.SteerInput("unused", UserInput{Text: "Review original records", HistoryReport: true})
	if err == nil || !strings.Contains(err.Error(), "idle turn") {
		t.Fatalf("explicit report mode was silently ignored: %v", err)
	}
}

func TestExplicitHistoryReportCannotMutateGoalOrPlanBeforeItsModeIsRecorded(t *testing.T) {
	for _, text := range []string{"/goal inspect retained records", "/plan inspect retained records"} {
		t.Run(text, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("Conflicting report command", "", "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.StartTurnInput(th.ID, UserInput{Text: text, HistoryReport: true})
			if err == nil {
				t.Fatal("explicit report mode silently became a command")
			}
			current, err := e.store.GetThread(th.ID)
			if err != nil || current.Goal != th.Goal || current.PlanMode != th.PlanMode {
				t.Fatalf("rejected report changed command state: %+v %v", current, err)
			}
		})
	}
}

func TestEditingAReportRequestRetainsItsExplicitEvidenceMode(t *testing.T) {
	for _, requested := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "report"}[requested], func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("Edited report request", "", "")
			if err != nil {
				t.Fatal(err)
			}
			first := mustStoredTurn(t, e, th.ID, store.Turn{UserText: "Original request"})
			if requested {
				if err := e.recordHistoryReportRequest(first); err != nil {
					t.Fatal(err)
				}
			}
			user := store.Event{ThreadID: th.ID, TurnID: first.ID, Kind: KindUser, Text: first.UserText}
			if err := e.store.AppendEvent(&user); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(first.ID, store.TurnDone, "", ""); err != nil {
				t.Fatal(err)
			}
			again, err := e.StartTurnInput(th.ID, UserInput{Text: "Edited request", FromEventSeq: user.Seq})
			if err != nil {
				t.Fatal(err)
			}
			waitForTurn(t, e, again.ID)
			got, err := e.store.TurnHasHistoryReportRequest(th.ID, again.ID)
			if err != nil || got != requested {
				t.Fatalf("edit lost the selected report mode: got=%v want=%v err=%v", got, requested, err)
			}
		})
	}
}

func TestReadingAnEditedReportModePreservesStorageErrors(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.historyReportRequestAt("missing", 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing edit source lost its cause: %v", err)
	}
}

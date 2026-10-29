package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func historyRegistrySuccessor(t *testing.T, f historyEvidenceFixture) Turn {
	t.Helper()
	turn := Turn{ThreadID: f.recipient.ID}
	if err := f.s.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	return turn
}

func TestHistoryRegistryInheritancePropagatesOnlyAlongTheRecordedRegistryChain(t *testing.T) {
	for _, mode := range []string{"history_read_started", "tool_call", "history_report_requested"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			if err := f.s.AppendEvent(&Event{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: mode, Text: "query_history({})"}); err != nil {
				t.Fatal(err)
			}
			second, third, unrelated := historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f)
			if seen, err := f.s.TurnHasHistoryQuery(second.ThreadID, second.ID); err != nil || seen {
				t.Fatalf("same conversation silently inherited without a Registry edge: %v %v", seen, err)
			}
			if err := f.s.RecordHistoryRegistryInheritance(t.Context(), second.ThreadID, second.ID, f.turn.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.s.RecordHistoryRegistryInheritance(t.Context(), third.ThreadID, third.ID, second.ID); err != nil {
				t.Fatal(err)
			}
			for _, turn := range []Turn{f.turn, second, third} {
				queried, err := f.s.TurnHasHistoryQuery(turn.ThreadID, turn.ID)
				if err != nil || queried != (mode != "history_report_requested") {
					t.Fatalf("ancestor query mode=%v %v for %s", queried, err, turn.ID)
				}
				requested, err := f.s.TurnHasHistoryReportRequest(turn.ThreadID, turn.ID)
				if err != nil || requested != (mode == "history_report_requested") {
					t.Fatalf("ancestor request mode=%v %v for %s", requested, err, turn.ID)
				}
			}
			for _, check := range []func(string, string) (bool, error){f.s.TurnHasHistoryQuery, f.s.TurnHasHistoryReportRequest} {
				if seen, err := check(unrelated.ThreadID, unrelated.ID); err != nil || seen {
					t.Fatalf("Registry inheritance became a conversation-wide sticky flag: %v %v", seen, err)
				}
			}
			events, err := f.s.ListTurnEvents(second.ID)
			if err != nil || len(events) != 1 || events[0].Kind != "history_registry_inherited" || events[0].ThreadID != second.ThreadID || events[0].SourceTurnID != f.turn.ID || events[0].Text != "" {
				t.Fatalf("inheritance invented source prose or lost real owners: %#v %v", events, err)
			}
		})
	}
}

func TestHistoryRegistryInheritanceIsIdempotentAndRefusesAConflictingParent(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	otherParent, target := historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f)
	const parallel = 8
	errorsSeen := make(chan error, parallel)
	var joined sync.WaitGroup
	for range parallel {
		joined.Add(1)
		go func() {
			defer joined.Done()
			errorsSeen <- f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, f.turn.ID)
		}()
	}
	joined.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := f.s.ListTurnEvents(target.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("concurrent same-edge calls duplicated the Registry transfer: %#v %v", events, err)
	}
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, otherParent.ID); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("same Registry target acquired an unrelated parent: %v", err)
	}
	duplicate := events[0]
	duplicate.ID = 0
	if err := f.s.DB().Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, f.turn.ID); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("ambiguous persisted parents were silently accepted: %v", err)
	}
}

func TestHistoryRegistryInheritanceRejectsMissingForeignAndNonPredecessorOwners(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	target := historyRegistrySuccessor(t, f)
	foreign := Turn{ThreadID: f.other.ID}
	if err := f.s.CreateTurn(&foreign); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][3]string{
		{"", target.ID, f.turn.ID}, {target.ThreadID, "", f.turn.ID}, {target.ThreadID, target.ID, ""},
		{target.ThreadID, target.ID, target.ID}, {target.ThreadID, f.turn.ID, target.ID},
		{target.ThreadID, target.ID, "missing-source"}, {target.ThreadID, "missing-target", f.turn.ID},
		{target.ThreadID, target.ID, foreign.ID}, {f.other.ID, target.ID, f.turn.ID},
	} {
		if err := f.s.RecordHistoryRegistryInheritance(t.Context(), args[0], args[1], args[2]); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
			t.Fatalf("invalid relation=%v err=%v", args, err)
		}
	}
	if err := f.s.DB().Model(&Turn{}).Where("id=?", f.turn.ID).Update("seq", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, f.turn.ID); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("unsequenced source was treated as a real predecessor: %v", err)
	}
	events, err := f.s.ListTurnEvents(target.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("invalid relation published an edge: %#v %v", events, err)
	}
}

func TestHistoryRegistryTraversalIgnoresCorruptForeignAndReversedEdges(t *testing.T) {
	for _, corrupt := range []string{"foreign-source", "reversed", "equal-seq", "wrong-event-thread", "missing-source", "cycle"} {
		t.Run(corrupt, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			target, later := historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f)
			foreign := Turn{ThreadID: f.other.ID}
			if err := f.s.CreateTurn(&foreign); err != nil {
				t.Fatal(err)
			}
			source := f.turn
			edge := Event{ThreadID: target.ThreadID, TurnID: target.ID, Kind: "history_registry_inherited", SourceTurnID: source.ID}
			switch corrupt {
			case "foreign-source":
				source, edge.SourceTurnID = foreign, foreign.ID
			case "reversed":
				source, edge.SourceTurnID = later, later.ID
			case "equal-seq", "cycle":
				if err := f.s.DB().Model(&Turn{}).Where("id=?", source.ID).Update("seq", target.Seq).Error; err != nil {
					t.Fatal(err)
				}
				if corrupt == "cycle" {
					if err := f.s.AppendEvent(&Event{ThreadID: source.ThreadID, TurnID: source.ID, Kind: edge.Kind, SourceTurnID: target.ID}); err != nil {
						t.Fatal(err)
					}
				}
			case "wrong-event-thread":
				edge.ThreadID = f.other.ID
			case "missing-source":
				edge.SourceTurnID = "missing-source"
			}
			if err := f.s.AppendEvent(&Event{ThreadID: source.ThreadID, TurnID: source.ID, Kind: "history_read_started"}); err != nil {
				t.Fatal(err)
			}
			if err := f.s.AppendEvent(&edge); err != nil {
				t.Fatal(err)
			}
			if seen, err := f.s.TurnHasHistoryQuery(target.ThreadID, target.ID); err != nil || seen {
				t.Fatalf("invalid relation selected unrelated/recursive history: %v %v", seen, err)
			}
		})
	}
}

func TestHistoryRegistryLineageSurvivesColdStoreReopenAndIdleAncestorReads(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	target := historyRegistrySuccessor(t, f)
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, f.turn.ID); err != nil {
		t.Fatal(err)
	}
	// The original worker can read while no successor manager is observing it.
	if err := f.s.AppendEvent(&Event{ThreadID: f.turn.ThreadID, TurnID: f.turn.ID, Kind: "history_read_started"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if seen, err := next.TurnHasHistoryQuery(target.ThreadID, target.ID); err != nil || !seen {
		t.Fatalf("cold restore lost the actual inherited Registry's original read: %v %v", seen, err)
	}
}

func TestHistoryRegistryInheritanceStorageFailuresAndCancellationCreateNoEdge(t *testing.T) {
	for _, stage := range []string{"read", "parent-read", "write", "zero-write", "canceled", "waiting-store"} {
		t.Run(stage, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			target := historyRegistrySuccessor(t, f)
			cause := errors.New("fixture Registry inheritance " + stage + " failure")
			ctx := t.Context()
			switch stage {
			case "read":
				if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_registry_read_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "turns" {
						_ = tx.AddError(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "parent-read":
				if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_registry_parent_failure", func(tx *gorm.DB) {
					if len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == "source_turn_id" {
						_ = tx.AddError(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "write", "zero-write":
				if err := f.s.DB().Callback().Create().After("gorm:create").Register("history_registry_write_failure", func(tx *gorm.DB) {
					if event, ok := tx.Statement.Dest.(*Event); ok && event.Kind == "history_registry_inherited" {
						if stage == "write" {
							_ = tx.AddError(cause)
						} else {
							tx.RowsAffected = 0
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(ctx)
				cancel(cause)
			}
			if stage == "waiting-store" {
				if err := f.s.AppendEvent(&Event{ThreadID: f.turn.ThreadID, TurnID: f.turn.ID, Kind: "fixture_seq_seed"}); err != nil {
					t.Fatal(err)
				}
				sqlDB, err := f.s.DB().DB()
				if err != nil {
					t.Fatal(err)
				}
				conn, err := sqlDB.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				err = f.s.RecordHistoryRegistryInheritance(ctx, target.ThreadID, target.ID, f.turn.ID)
				cancel()
				if closeErr := conn.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("waiting edge ignored cancellation: %v", err)
				}
			} else {
				err := f.s.RecordHistoryRegistryInheritance(ctx, target.ThreadID, target.ID, f.turn.ID)
				if err == nil || stage != "zero-write" && !errors.Is(err, cause) || errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
					t.Fatalf("storage/cancellation was swallowed or relabeled as invalid lineage: %v", err)
				}
			}
			events, err := f.s.ListTurnEvents(target.ID)
			if err != nil || len(events) != 0 {
				t.Fatalf("failed Registry relation became durable: %#v %v", events, err)
			}
		})
	}
}

func TestHistoryRegistryInheritanceCannotProceedAfterSequenceSeedFailure(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	target := historyRegistrySuccessor(t, f)
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), target.ThreadID, target.ID, f.turn.ID); err == nil || errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("sequence seed storage failure was swallowed or called an invalid relation: %v", err)
	}
}

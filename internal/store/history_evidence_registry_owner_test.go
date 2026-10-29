package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestHistoryRegistryReceiptPublishesOnlyANewCommittedIdentity(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	target := historyRegistrySuccessor(t, f)
	event, created, err := f.s.RecordHistoryRegistryInheritanceEvent(t.Context(), target.ThreadID, target.ID, f.turn.ID)
	if err != nil || !created || event == nil || event.ID == 0 || event.Seq <= 0 || event.CreatedAt.IsZero() || event.ThreadID != target.ThreadID || event.TurnID != target.ID || event.SourceTurnID != f.turn.ID || event.Kind != "history_registry_inherited" || event.Text != "" {
		t.Fatalf("new durable edge lost its exact publication receipt: %+v %v %v", event, created, err)
	}
	stored, err := f.s.ListTurnEvents(target.ID)
	if err != nil || len(stored) != 1 || stored[0].ID != event.ID || stored[0].Seq != event.Seq {
		t.Fatalf("receipt was not the committed edge: %#v %+v %v", stored, event, err)
	}
	if event, created, err := f.s.RecordHistoryRegistryInheritanceEvent(t.Context(), target.ThreadID, target.ID, f.turn.ID); err != nil || created || event != nil {
		t.Fatalf("idempotent admission requested duplicate broadcast: %+v %v %v", event, created, err)
	}
}

func TestHistoryRegistryFailedReceiptCannotRequestPublication(t *testing.T) {
	for _, stage := range []string{"invalid", "write", "zero-write", "commit-canceled"} {
		t.Run(stage, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			target := historyRegistrySuccessor(t, f)
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			cause := errors.New("fixture failed receipt " + stage)
			if stage != "invalid" {
				if err := f.s.DB().Callback().Create().After("gorm:create").Register("history_failed_publication_receipt", func(tx *gorm.DB) {
					if event, ok := tx.Statement.Dest.(*Event); ok && event.Kind == "history_registry_inherited" {
						switch stage {
						case "write":
							_ = tx.AddError(cause)
						case "zero-write":
							tx.RowsAffected = 0
						case "commit-canceled":
							cancel(cause)
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				target.ID = f.turn.ID
			}
			if event, created, err := f.s.RecordHistoryRegistryInheritanceEvent(ctx, target.ThreadID, target.ID, f.turn.ID); err == nil || event != nil || created {
				t.Fatalf("failed/cancelled commit supplied a live publication receipt: %+v %v %v", event, created, err)
			}
			if events, err := f.s.ListTurnEvents(target.ID); err != nil || len(events) != 0 {
				t.Fatalf("failed receipt commit retained a partial edge: %#v %v", events, err)
			}
		})
	}
}

func TestProvenHistoryRegistryOwnerUsesOnlyTheGivenOwnersAndRealEdges(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	second, third, unrelated := historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f), historyRegistrySuccessor(t, f)
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), second.ThreadID, second.ID, f.turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordHistoryRegistryInheritance(t.Context(), third.ThreadID, third.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{nil, ""}, {[]string{}, ""}, {[]string{f.turn.ID}, f.turn.ID}, {[]string{f.turn.ID, f.turn.ID}, f.turn.ID},
		{[]string{f.turn.ID, second.ID}, second.ID}, {[]string{third.ID, f.turn.ID}, third.ID},
		{[]string{second.ID, third.ID, f.turn.ID, third.ID}, third.ID}, {[]string{unrelated.ID}, unrelated.ID},
	} {
		if got, err := f.s.ProvenHistoryRegistryOwner(t.Context(), f.recipient.ID, tc.ids); err != nil || got != tc.want {
			t.Fatalf("given owners=%v selected=%s want=%s err=%v", tc.ids, got, tc.want, err)
		}
	}
	// There is a newer real turn, but no evidence it inherited this Registry.
	if got, err := f.s.ProvenHistoryRegistryOwner(t.Context(), f.recipient.ID, []string{unrelated.ID, f.turn.ID}); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) || got != "" {
		t.Fatalf("a latest unrelated turn was guessed as the Registry owner: %s %v", got, err)
	}
}

func TestProvenHistoryRegistryOwnerRefusesMissingForeignAndBrokenLineage(t *testing.T) {
	for _, broken := range []string{"empty-owner", "missing", "foreign", "zero-seq", "missing-edge", "reversed-edge", "equal-seq", "foreign-edge", "empty-thread"} {
		t.Run(broken, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			second := historyRegistrySuccessor(t, f)
			threadID := f.recipient.ID
			ids := []string{f.turn.ID, second.ID}
			foreign := Turn{ThreadID: f.other.ID}
			if err := f.s.CreateTurn(&foreign); err != nil {
				t.Fatal(err)
			}
			switch broken {
			case "empty-owner":
				ids = []string{""}
			case "missing":
				ids = []string{f.turn.ID, "missing-owner"}
			case "foreign":
				ids = []string{foreign.ID}
			case "zero-seq":
				ids = []string{f.turn.ID}
				if err := f.s.DB().Model(&Turn{}).Where("id=?", f.turn.ID).Update("seq", 0).Error; err != nil {
					t.Fatal(err)
				}
			case "equal-seq":
				if err := f.s.DB().Model(&Turn{}).Where("id=?", f.turn.ID).Update("seq", second.Seq).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.s.AppendEvent(&Event{ThreadID: threadID, TurnID: second.ID, SourceTurnID: f.turn.ID, Kind: "history_registry_inherited"}); err != nil {
					t.Fatal(err)
				}
			case "foreign-edge":
				if err := f.s.AppendEvent(&Event{ThreadID: f.other.ID, TurnID: second.ID, SourceTurnID: f.turn.ID, Kind: "history_registry_inherited"}); err != nil {
					t.Fatal(err)
				}
			case "reversed-edge":
				if err := f.s.AppendEvent(&Event{ThreadID: threadID, TurnID: f.turn.ID, SourceTurnID: second.ID, Kind: "history_registry_inherited"}); err != nil {
					t.Fatal(err)
				}
			case "empty-thread":
				threadID = ""
			}
			if got, err := f.s.ProvenHistoryRegistryOwner(t.Context(), threadID, ids); !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) || got != "" {
				t.Fatalf("unproven owners=%v got=%s err=%v", ids, got, err)
			}
		})
	}
}

func TestProvenHistoryRegistryOwnerPreservesStorageAndCancellationCauses(t *testing.T) {
	for _, stage := range []string{"turn-read", "lineage-read", "canceled", "corrupt-edge-scope"} {
		t.Run(stage, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			second := historyRegistrySuccessor(t, f)
			if err := f.s.RecordHistoryRegistryInheritance(t.Context(), second.ThreadID, second.ID, f.turn.ID); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			cause := errors.New("fixture owner proof " + stage + " failed")
			switch stage {
			case "turn-read":
				if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_owner_proof_read_failure", func(tx *gorm.DB) { _ = tx.AddError(cause) }); err != nil {
					t.Fatal(err)
				}
			case "lineage-read":
				if err := f.s.DB().Callback().Row().Before("gorm:row").Register("history_owner_proof_lineage_failure", func(tx *gorm.DB) { _ = tx.AddError(cause) }); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(ctx)
				cancel(cause)
			case "corrupt-edge-scope":
				if err := f.s.DB().Model(&Turn{}).Where("id=?", f.turn.ID).Update("thread_id", f.other.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.s.ProvenHistoryRegistryOwner(ctx, f.recipient.ID, []string{f.turn.ID, second.ID})
			if stage == "corrupt-edge-scope" {
				if got != "" || !errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
					t.Fatalf("changed owner scope proved a Registry: %s %v", got, err)
				}
			} else if got != "" || !errors.Is(err, cause) || errors.Is(err, ErrHistoryRegistryInheritanceInvalid) {
				t.Fatalf("storage/cancel lost its real cause: %s %v", got, err)
			}
		})
	}
}

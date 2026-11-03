package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func appendNuwaToolEvent(t *testing.T, s *Store, owner NuwaNode, kind, callID string) NuwaEvidence {
	t.Helper()
	return appendScopedNuwaToolEvent(t, s, owner, kind, callID, "")
}

func appendScopedNuwaToolEvent(t *testing.T, s *Store, owner NuwaNode, kind, callID, scope string) NuwaEvidence {
	t.Helper()
	ev := Event{ThreadID: owner.ThreadID, TurnID: owner.TurnID, AgentID: owner.AgentID,
		WorkerGeneration: owner.Generation, Kind: kind, ToolCallID: callID, ToolReceiptScope: scope, Text: "private tool transcript"}
	if err := s.AppendEvent(&ev); err != nil {
		t.Fatal(err)
	}
	return NuwaEvidence{ThreadID: ev.ThreadID, TurnID: ev.TurnID, Seq: ev.Seq}
}

func appendNuwaToolPair(t *testing.T, s *Store, owner NuwaNode, callID string) NuwaEvidence {
	t.Helper()
	appendNuwaToolEvent(t, s, owner, "tool_call", callID)
	return appendNuwaToolEvent(t, s, owner, "tool_result", callID)
}

func TestNuwaScopedToolEvidenceFollowsResultIntoAdoptedTurn(t *testing.T) {
	f := newNuwaFixture(t)
	owner := f.node("fact")
	const id, scope = "read", "original-execution"
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", id, scope)
	adopted := Turn{ThreadID: owner.ThreadID}
	if err := f.s.CreateTurn(&adopted); err != nil {
		t.Fatal(err)
	}
	observer := owner
	observer.TurnID = adopted.ID
	// This independent execution reused the same agent ID, generation and
	// provider call ID. It cannot contribute either half of the pair.
	appendScopedNuwaToolEvent(t, f.s, observer, "tool_call", id, "another-execution")
	appendScopedNuwaToolEvent(t, f.s, observer, "tool_result", id, "another-execution")
	result := appendScopedNuwaToolEvent(t, f.s, observer, "tool_result", id, scope)
	receipt := NuwaWriteReceipt{Key: "receipt", Hash: "input", OwnToolCallIDs: []string{id}, CurrentToolCallID: "record", ToolReceiptScope: scope}
	if err := f.s.UpsertNuwaNode(t.Context(), &owner, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if owner.TurnID != f.turn.ID || !slices.Equal(owner.Evidence, []NuwaEvidence{result}) {
		t.Fatalf("write owner or evidence observation turn changed: %+v", owner)
	}
	rev, nodes, err := f.s.NuwaSnapshot(t.Context(), f.project.ID)
	if err != nil || rev != 1 || len(nodes) != 1 ||
		!slices.Equal(nodes[0].Evidence, []NuwaEvidence{result}) || len(nodes[0].UnavailableEvidence) != 0 {
		t.Fatalf("scoped reference did not remain available: revision=%d nodes=%+v err=%v", rev, nodes, err)
	}
	encoded, err := json.Marshal(Event{ToolReceiptScope: scope})
	if err != nil || strings.Contains(string(encoded), scope) || strings.Contains(string(encoded), "tool_receipt_scope") {
		t.Fatalf("serialized event leaked internal execution identity: %s, %v", encoded, err)
	}
}

func TestNuwaScopedToolEvidenceDoesNotPairAcrossExecutions(t *testing.T) {
	f := newNuwaFixture(t)
	owner := f.node("fact")
	const old, current = "old-execution", "current-execution"
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", "missing-current-call", old)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", "missing-current-call", current)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", "missing-current-result", current)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", "missing-current-result", old)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", "old-pair", old)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", "old-pair", old)
	appendNuwaToolPair(t, f.s, owner, "legacy-pair")
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", "duplicate-current", current)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", "duplicate-current", current)
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", "duplicate-current", current)
	otherGeneration := owner
	otherGeneration.Generation++
	appendScopedNuwaToolEvent(t, f.s, otherGeneration, "tool_call", "different-generation", current)
	appendScopedNuwaToolEvent(t, f.s, otherGeneration, "tool_result", "different-generation", current)
	otherActor := owner
	otherActor.AgentID = "other-worker"
	appendScopedNuwaToolEvent(t, f.s, otherActor, "tool_call", "different-actor", current)
	appendScopedNuwaToolEvent(t, f.s, otherActor, "tool_result", "different-actor", current)

	for _, id := range []string{"missing-current-call", "missing-current-result", "old-pair", "legacy-pair", "duplicate-current", "different-generation", "different-actor"} {
		t.Run(id, func(t *testing.T) {
			node := owner
			receipt := NuwaWriteReceipt{Key: id, Hash: "input", OwnToolCallIDs: []string{id}, CurrentToolCallID: "record", ToolReceiptScope: current}
			if err := f.s.UpsertNuwaNode(t.Context(), &node, 0, receipt); !errors.Is(err, ErrNuwaOwnToolReceiptUnavailable) {
				t.Fatalf("expected execution-scope refusal, got %v", err)
			}
		})
	}
	if rev, nodes, err := f.s.NuwaSnapshot(t.Context(), f.project.ID); err != nil || rev != 0 || len(nodes) != 0 {
		t.Fatalf("refused scopes changed revision=%d nodes=%+v err=%v", rev, nodes, err)
	}
}

func TestNuwaUnscopedManagerToolEvidenceStaysInItsTurn(t *testing.T) {
	f := newNuwaFixture(t)
	owner := f.node("fact")
	owner.AgentID, owner.Generation = "manager", 0
	const id = "read"
	result := appendNuwaToolPair(t, f.s, owner, id)
	// A database migrated from before execution scopes stores NULL for its
	// existing events. That history remains addressable by an unscoped manager.
	if err := f.s.DB().Model(&Event{}).Where("thread_id = ? AND tool_call_id = ?", owner.ThreadID, id).Update("tool_receipt_scope", nil).Error; err != nil {
		t.Fatal(err)
	}
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_call", id, "unrelated-execution")
	appendScopedNuwaToolEvent(t, f.s, owner, "tool_result", id, "unrelated-execution")
	later := Turn{ThreadID: owner.ThreadID}
	if err := f.s.CreateTurn(&later); err != nil {
		t.Fatal(err)
	}
	observer := owner
	observer.TurnID = later.ID
	appendNuwaToolPair(t, f.s, observer, id)
	receipt := NuwaWriteReceipt{Key: "receipt", Hash: "input", OwnToolCallIDs: []string{id}, CurrentToolCallID: "record"}
	if err := f.s.UpsertNuwaNode(t.Context(), &owner, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owner.Evidence, []NuwaEvidence{result}) {
		t.Fatalf("unscoped manager bound evidence outside its exact turn: %+v", owner.Evidence)
	}
}

func TestNuwaOwnToolEvidenceUsesUpdatingWorkerAndKeepsOrigin(t *testing.T) {
	f := newNuwaFixture(t)
	ctx := t.Context()
	node := f.node("fact")
	node.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: f.event.Seq, Label: "literal reference"}}
	if err := f.s.UpsertNuwaNode(ctx, &node, 0); err != nil {
		t.Fatal(err)
	}

	update := node
	update.AgentID, update.Generation = "reviewer", 3
	first := appendNuwaToolPair(t, f.s, update, "first-read")
	second := appendNuwaToolPair(t, f.s, update, "second-read")
	receipt := NuwaWriteReceipt{
		Key: "update-receipt", Hash: "update-input", OwnToolCallIDs: []string{"second-read", "first-read"},
		CurrentToolCallID: "record-update",
	}
	if err := f.s.UpsertNuwaNode(ctx, &update, node.Version, receipt); err != nil {
		t.Fatal(err)
	}
	want := []NuwaEvidence{second, first}
	if !slices.Equal(update.Evidence, want) {
		t.Fatalf("resolved evidence = %+v, want %+v", update.Evidence, want)
	}
	if update.AgentID != node.AgentID || update.Generation != node.Generation ||
		update.UpdatedBy != "reviewer" || update.UpdatedGeneration != 3 {
		t.Fatalf("worker evidence update changed origin or lost correction identity: %+v", update)
	}
	revision, nodes, err := f.s.NuwaSnapshot(ctx, f.project.ID)
	if err != nil || revision != 2 || len(nodes) != 1 || !slices.Equal(nodes[0].Evidence, want) {
		t.Fatalf("durable revision/evidence = %d/%+v, err = %v", revision, nodes, err)
	}

	clear := update
	clear.AgentID, clear.Generation = "reviewer", 3
	receipt = NuwaWriteReceipt{Key: "clear-receipt", Hash: "clear-input", OwnToolCallIDs: []string{}, CurrentToolCallID: "record-clear"}
	if err := f.s.UpsertNuwaNode(ctx, &clear, update.Version, receipt); err != nil {
		t.Fatal(err)
	}
	if clear.Evidence == nil || len(clear.Evidence) != 0 {
		t.Fatalf("explicit empty ID list did not clear evidence: %+v", clear.Evidence)
	}
	revision, nodes, err = f.s.NuwaSnapshot(ctx, f.project.ID)
	if err != nil || revision != 3 || len(nodes) != 1 || nodes[0].Evidence == nil || len(nodes[0].Evidence) != 0 {
		t.Fatalf("durable evidence clear = %d/%+v, err = %v", revision, nodes, err)
	}
}

func TestNuwaOwnToolEvidenceRejectsUnresolvedOrAmbiguousPairsAtomically(t *testing.T) {
	f := newNuwaFixture(t)
	ctx := t.Context()
	owner := f.node("fact")
	appendNuwaToolPair(t, f.s, owner, "valid")
	appendNuwaToolPair(t, f.s, owner, "record")
	appendNuwaToolEvent(t, f.s, owner, "tool_call", "pending")
	appendNuwaToolEvent(t, f.s, owner, "tool_result", "unpaired-result")
	appendNuwaToolEvent(t, f.s, owner, "tool_result", "out-of-order")
	appendNuwaToolEvent(t, f.s, owner, "tool_call", "out-of-order")
	appendNuwaToolPair(t, f.s, owner, "duplicate-result")
	appendNuwaToolEvent(t, f.s, owner, "tool_result", "duplicate-result")
	appendNuwaToolEvent(t, f.s, owner, "tool_call", "duplicate-call")
	appendNuwaToolPair(t, f.s, owner, "duplicate-call")
	appendNuwaToolEvent(t, f.s, owner, "tool_call", "only-duplicate-calls")
	appendNuwaToolEvent(t, f.s, owner, "tool_call", "only-duplicate-calls")

	sibling := owner
	sibling.AgentID = "other-worker"
	appendNuwaToolPair(t, f.s, sibling, "sibling")
	older := owner
	older.Generation--
	appendNuwaToolPair(t, f.s, older, "older-generation")

	otherTurn := Turn{ThreadID: f.thread.ID}
	if err := f.s.CreateTurn(&otherTurn); err != nil {
		t.Fatal(err)
	}
	otherOwner := owner
	otherOwner.TurnID = otherTurn.ID
	appendNuwaToolPair(t, f.s, otherOwner, "other-turn")

	foreign := Project{Name: "foreign"}
	if err := f.s.CreateProject(&foreign); err != nil {
		t.Fatal(err)
	}
	foreignThread := Thread{ProjectID: foreign.ID}
	if err := f.s.CreateThread(&foreignThread); err != nil {
		t.Fatal(err)
	}
	foreignTurn := Turn{ThreadID: foreignThread.ID}
	if err := f.s.CreateTurn(&foreignTurn); err != nil {
		t.Fatal(err)
	}
	foreignOwner := owner
	foreignOwner.ThreadID, foreignOwner.TurnID = foreignThread.ID, foreignTurn.ID
	appendNuwaToolPair(t, f.s, foreignOwner, "foreign-thread")

	tooMany := make([]string, 17)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("receipt-%d", i)
		appendNuwaToolPair(t, f.s, owner, tooMany[i])
	}
	for _, tc := range []struct {
		name, current string
		ids           []string
	}{
		{"empty-id", "record", []string{""}},
		{"blank-id", "record", []string{" "}},
		{"exact-match", "record", []string{" valid"}},
		{"duplicate-id", "record", []string{"valid", "valid"}},
		{"over-limit", "record", tooMany},
		{"current", "record", []string{"record"}},
		{"missing-current", "", []string{"valid"}},
		{"missing", "record", []string{"unknown"}},
		{"partial", "record", []string{"valid", "unknown"}},
		{"pending", "record", []string{"pending"}},
		{"unpaired-result", "record", []string{"unpaired-result"}},
		{"out-of-order", "record", []string{"out-of-order"}},
		{"duplicate-call", "record", []string{"duplicate-call"}},
		{"duplicate-result", "record", []string{"duplicate-result"}},
		{"only-duplicate-calls", "record", []string{"only-duplicate-calls"}},
		{"sibling", "record", []string{"sibling"}},
		{"older-generation", "record", []string{"older-generation"}},
		{"other-turn", "record", []string{"other-turn"}},
		{"foreign-thread", "record", []string{"foreign-thread"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := owner
			receipt := NuwaWriteReceipt{Key: "reject-" + tc.name, Hash: "input", OwnToolCallIDs: tc.ids, CurrentToolCallID: tc.current}
			if err := f.s.UpsertNuwaNode(ctx, &node, 0, receipt); !errors.Is(err, ErrInvalidNuwaNode) || !errors.Is(err, ErrNuwaOwnToolReceiptUnavailable) {
				t.Fatalf("expected evidence refusal, got %v", err)
			}
			if node.ID != "" || node.Version != 0 {
				t.Fatalf("rejected write mutated its input: %+v", node)
			}
		})
	}
	if revision, nodes, err := f.s.NuwaSnapshot(ctx, f.project.ID); err != nil || revision != 0 || len(nodes) != 0 {
		t.Fatalf("refusals committed revision %d and %d nodes: %v", revision, len(nodes), err)
	}
}

func TestNuwaOwnToolEvidenceReadsIndexedMetadataWithoutToolBodies(t *testing.T) {
	for _, tc := range []struct{ name, scope, index string }{
		{"manager", "", "idx_evt_thread_turn_kind_seq"},
		{"worker", "worker-execution", "idx_evt_tool_receipt_scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNuwaFixture(t)
			node := f.node("fact")
			if tc.scope == "" {
				node.AgentID, node.Generation = "manager", 0
			}
			appendScopedNuwaToolEvent(t, f.s, node, "tool_call", "read", tc.scope)
			appendScopedNuwaToolEvent(t, f.s, node, "tool_result", "read", tc.scope)
			var indexSQL string
			if err := f.s.DB().Raw("SELECT sql FROM sqlite_schema WHERE type = 'index' AND name = ?", "idx_evt_tool_receipt_scope").Scan(&indexSQL).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(indexSQL, "WHERE tool_receipt_scope <> ''") {
				t.Fatalf("worker receipt index is not partial: %s", indexSQL)
			}
			var query string
			var args []any
			var projections [][]string
			if err := f.s.DB().Callback().Query().After("gorm:query").Register("test:observe_nuwa_own_evidence", func(tx *gorm.DB) {
				if tx.Statement.Table == "events" {
					query = tx.Statement.SQL.String()
					args = slices.Clone(tx.Statement.Vars)
					projections = append(projections, slices.Clone(tx.Statement.Selects))
				}
			}); err != nil {
				t.Fatal(err)
			}
			receipt := NuwaWriteReceipt{
				Key: "receipt", Hash: "input", OwnToolCallIDs: []string{"read"},
				CurrentToolCallID: "record", ToolReceiptScope: tc.scope,
			}
			if err := f.s.UpsertNuwaNode(t.Context(), &node, 0, receipt); err != nil {
				t.Fatal(err)
			}
			if len(projections) != 1 || !slices.Equal(projections[0], []string{"tool_call_id", "kind", "seq", "turn_id"}) {
				t.Fatalf("receipt resolution loaded unexpected event columns: %v", projections)
			}
			var plan []struct{ Detail string }
			if err := f.s.DB().Raw("EXPLAIN QUERY PLAN "+query, args...).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(plan, func(row struct{ Detail string }) bool {
				return strings.Contains(row.Detail, tc.index)
			}) {
				t.Fatalf("receipt resolution did not use %s: %v", tc.index, plan)
			}
		})
	}
}

func TestNuwaOwnToolEvidenceReplaysCommittedReferenceAfterSourceRemoval(t *testing.T) {
	f := newNuwaFixture(t)
	ctx := t.Context()
	input := f.node("fact")
	appendScopedNuwaToolEvent(t, f.s, input, "tool_call", "observed", "original-execution")
	want := appendScopedNuwaToolEvent(t, f.s, input, "tool_result", "observed", "original-execution")
	receipt := NuwaWriteReceipt{
		Key: "receipt", Hash: "input", OwnToolCallIDs: []string{"observed"},
		CurrentToolCallID: "record", ToolReceiptScope: "original-execution",
	}
	committed := input
	if err := f.s.UpsertNuwaNode(ctx, &committed, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Where("thread_id = ? AND seq = ?", want.ThreadID, want.Seq).Delete(&Event{}).Error; err != nil {
		t.Fatal(err)
	}
	// Recovery gives fresh executions a new scope without changing the
	// idempotent result of an already committed record_work request.
	receipt.ToolReceiptScope = "recovered-execution"
	if err := f.s.UpsertNuwaNode(ctx, &input, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if input.ID != committed.ID || input.Version != committed.Version || !slices.Equal(input.Evidence, []NuwaEvidence{want}) {
		t.Fatalf("replay lost the original durable evidence: %+v", input)
	}
	changed := f.node("fact")
	receipt.Hash = "different-input"
	if err := f.s.UpsertNuwaNode(ctx, &changed, 0, receipt); !errors.Is(err, ErrNuwaConflict) {
		t.Fatalf("changed input unexpectedly replayed the original receipt: %v", err)
	}
	revision, nodes, err := f.s.NuwaSnapshot(ctx, f.project.ID)
	if err != nil || revision != 1 || len(nodes) != 1 || !slices.Equal(nodes[0].UnavailableEvidence, []NuwaEvidence{want}) {
		t.Fatalf("replay or source removal changed history rev=%d nodes=%+v err=%v", revision, nodes, err)
	}
}

func TestNuwaOwnToolEvidenceReadFailureDoesNotCommit(t *testing.T) {
	f := newNuwaFixture(t)
	node := f.node("fact")
	appendNuwaToolPair(t, f.s, node, "read")
	unavailable := errors.New("receipt read failed")
	if err := f.s.DB().Callback().Query().Before("gorm:query").Register("test:fail_nuwa_own_evidence", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			tx.AddError(unavailable)
		}
	}); err != nil {
		t.Fatal(err)
	}
	receipt := NuwaWriteReceipt{Key: "receipt", Hash: "input", OwnToolCallIDs: []string{"read"}, CurrentToolCallID: "record"}
	if err := f.s.UpsertNuwaNode(t.Context(), &node, 0, receipt); !errors.Is(err, unavailable) {
		t.Fatalf("expected original store failure, got %v", err)
	}
	if revision, nodes, err := f.s.NuwaSnapshot(t.Context(), f.project.ID); err != nil || revision != 0 || len(nodes) != 0 {
		t.Fatalf("failed read committed revision %d and %d nodes: %v", revision, len(nodes), err)
	}
}

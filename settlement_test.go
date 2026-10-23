package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWaitTimeoutDoesNotCollectARunningHTTPWorker(t *testing.T) {
	hold := make(chan struct{})
	r := newSettlementHTTPRegistry(t, hold)
	id, h := spawnSettlementWorker(t, r)
	out, err := invokable(t, r.Tools()[2]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":1}`, id))
	if err != nil {
		t.Fatal(err)
	}
	var report waitReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if !report.TimedOut || len(report.Agents) != 1 || report.Agents[0].Status != "running" || len(r.PendingAgents()) != 1 {
		t.Fatalf("timeout consumed ongoing work: %s pending=%+v", out, r.PendingAgents())
	}
	close(hold)
	waitSettlementDone(t, h)
	if len(r.PendingAgents()) != 1 {
		t.Fatal("natural completion was mistaken for result collection")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := invokable(t, r.Tools()[2]).InvokableRun(ctx, fmt.Sprintf(`{"agent_ids":[%q]}`, id)); !errors.Is(err, context.Canceled) || len(r.PendingAgents()) != 1 {
		t.Fatalf("a cancelled wait consumed a result: %v %+v", err, r.PendingAgents())
	}
}

func TestWaitCollectsOnlyCompletedEntriesActuallyReturned(t *testing.T) {
	r := newSettlementHTTPRegistry(t, nil)
	quick, h := spawnSettlementWorker(t, r)
	waitSettlementDone(t, h)
	hold := make(chan struct{})
	slowSource := newSettlementHTTPRegistry(t, hold)
	t.Cleanup(func() { close(hold) })
	r.ModelBuilder = slowSource.ModelBuilder
	slow, err := r.Spawn(t.Context(), "second", "inspect another operation", r.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	out, err := invokable(t, r.Tools()[2]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_ids":[%q,%q,"absent"],"timeout_s":1}`, quick, slow.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"status":"done"`) || !strings.Contains(out, `"status":"running"`) || !strings.Contains(out, `"status":"unknown"`) {
		t.Fatalf("wait did not return the actual mixed state: %s", out)
	}
	if pending := r.PendingAgents(); len(pending) != 1 || pending[0].AgentID != slow.ID || !pending[0].Running {
		t.Fatalf("one finished worker consumed another one's obligation: %+v", pending)
	}
}

func TestResumeReopensCollectionWithoutLettingAnOldSnapshotConsumeIt(t *testing.T) {
	r := newSettlementHTTPRegistry(t, nil)
	id, h := spawnSettlementWorker(t, r)
	waitSettlementDone(t, h)
	_, old := r.waitSnapshotCollections([]string{id}, false)
	if len(old) != 1 {
		t.Fatal("missing completed generation snapshot")
	}
	r.Stats()
	out, err := invokable(t, r.Tools()[4]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_id":%q,"task":"continue the assigned inspection"}`, id))
	if err != nil || !strings.Contains(out, `"generation":2`) {
		t.Fatalf("resume did not advance the same identity: %s %v", out, err)
	}
	if r.collectWorker(old[0], false) || r.collectWorker(old[0], true) {
		t.Fatal("an old wait/close snapshot settled a resumed generation")
	}
	current, ok := r.get(id)
	if !ok {
		t.Fatal("resumed ID was lost")
	}
	waitSettlementDone(t, current)
	if pending := r.PendingAgents(); len(pending) != 1 || pending[0].AgentID != id || pending[0].Generation != 2 {
		t.Fatalf("resume did not renew the collection obligation: %+v", pending)
	}
	if _, err := invokable(t, r.Tools()[2]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_ids":[%q]}`, id)); err != nil || len(r.PendingAgents()) != 0 {
		t.Fatalf("new result could not be collected: %v %+v", err, r.PendingAgents())
	}
	_, previous := r.waitSnapshotCollections([]string{id}, false)
	if _, err := invokable(t, r.Tools()[4]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_id":%q,"task":"verify the next operation"}`, id)); err != nil {
		t.Fatal(err)
	}
	if r.collectWorker(previous[0], false) || r.collectWorker(previous[0], true) {
		t.Fatal("a prior generation consumed or cancelled an in-place resume")
	}
	waitSettlementDone(t, current)
	if pending := r.PendingAgents(); len(pending) != 1 || pending[0].Generation != 3 {
		t.Fatalf("in-place resume lost its renewed obligation: %+v", pending)
	}
}

func TestCloseSettlesItsGenerationAndPreservesAnEarlierFailure(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	r := newSettlementHTTPRegistry(t, hold, entered)
	notifications := make(chan Notification, 32)
	r.SetHostNotify(func(n Notification) { notifications <- n })
	id, h := spawnSettlementWorker(t, r)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never reached its actual HTTP request")
	}
	out, err := invokable(t, r.Tools()[3]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_id":%q}`, id))
	if err != nil || !strings.Contains(out, `"cancelled":true`) || !strings.Contains(out, `"generation":1`) || len(r.PendingAgents()) != 0 {
		t.Fatalf("explicit close did not resolve its worker: %s %v %+v", out, err, r.PendingAgents())
	}
	waitSettlementDone(t, h)
	if _, cause, done := h.Result(); !done || cause == nil || !strings.Contains(cause.Error(), "cancel") {
		t.Fatalf("explicit close lost its cancellation cause: done=%v error=%v", done, cause)
	}
	if snap := r.SnapshotWorkers(); len(snap) != 1 || !snap[0].CloseRequested || snap[0].Collected {
		t.Fatalf("close intent was not retained separately: %+v", snap)
	}
	interrupted := false
	for len(notifications) != 0 {
		n := <-notifications
		if n.Kind == NotifyGenerationInterrupted && n.AgentID == id && n.MessageID != "" && strings.Contains(n.Text, `"reason":"cancelled"`) && strings.Contains(n.Text, `"recovery":"none"`) {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatal("actual close cancellation lost its generation cause or recovery state")
	}
	cause := errors.New("the provider refused this generation")
	if err := r.PlantFinished(FinishedWorker{ID: "failed-worker", Role: "reviewer", Generation: 3, Err: cause}); err != nil {
		t.Fatal(err)
	}
	if pending := r.PendingAgents(); len(pending) != 1 || pending[0].Running || pending[0].Err != cause.Error() {
		t.Fatalf("uncollected failure lost its observed cause: %+v", pending)
	}
	out, err = invokable(t, r.Tools()[3]).InvokableRun(t.Context(), `{"agent_id":"failed-worker"}`)
	if err != nil || !strings.Contains(out, `"already_finished":true`) || !strings.Contains(out, `"cancelled":false`) || len(r.PendingAgents()) != 0 {
		t.Fatalf("finished close fabricated cancellation or did not discard: %s %v", out, err)
	}
	failed, _ := r.get("failed-worker")
	if _, got, _ := failed.Result(); got != cause {
		t.Fatalf("discard overwrote the observed failure: %v", got)
	}
	if err := r.PlantFinished(FinishedWorker{ID: "still-uncollected", Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := invokable(t, r.Tools()[3]).InvokableRun(t.Context(), `{"agent_id":"absent"}`); err != nil || len(r.PendingAgents()) != 1 {
		t.Fatal("a rejected close consumed an unrelated result")
	}
}

func TestUncollectedResultsSurviveArchiveEvictionAndCheckpointRestore(t *testing.T) {
	r := NewRegistry()
	t.Cleanup(r.Close)
	r.ArchiveLimit = 1
	if err := r.PlantFinished(FinishedWorker{ID: "legacy", Result: "historically complete"}); err != nil || len(r.PendingAgents()) != 0 {
		t.Fatal("legacy completed history acquired a new obligation")
	}
	for _, id := range []string{"first", "second", "third"} {
		if err := r.PlantFinished(FinishedWorker{ID: id, Role: id, Result: "actual result", Generation: 4}); err != nil {
			t.Fatal(err)
		}
		r.Stats()
	}
	if len(r.PendingAgents()) != 3 || len(r.SnapshotWorkers()) != 3 {
		t.Fatalf("archive limits lost current uncollected results: %+v %+v", r.PendingAgents(), r.SnapshotWorkers())
	}
	if _, err := r.recall("first"); err != nil || r.reusableFinishedID("first") != "first" {
		t.Fatalf("evicted uncollected identity was no longer resumable: %v", err)
	}
	checkpoint := r.SnapshotWorkers()[0]
	next := NewRegistry()
	t.Cleanup(next.Close)
	if err := next.PlantFinished(FinishedWorker{ID: checkpoint.ID, Role: checkpoint.Role, Generation: checkpoint.Generation, Result: checkpoint.Result, Collected: checkpoint.Collected, CloseRequested: checkpoint.CloseRequested}); err != nil || len(next.PendingAgents()) != 1 {
		t.Fatalf("checkpoint dropped an uncollected generation: %v %+v", err, next.PendingAgents())
	}
	if _, err := invokable(t, next.Tools()[2]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_ids":[%q]}`, checkpoint.ID)); err != nil {
		t.Fatal(err)
	}
	if snap := next.SnapshotWorkers()[0]; !snap.Collected || snap.Generation != 4 {
		t.Fatalf("collection checkpoint was not updated: %+v", snap)
	}
	next.Close()
	if len(next.PendingAgents()) != 0 {
		t.Fatal("closed registry retained obligations")
	}
}

func TestRestoredRunningGenerationRetainsItsCollectionAndCloseIntent(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprint(closed), func(t *testing.T) {
			hold := make(chan struct{})
			r := newSettlementHTTPRegistry(t, hold)
			h, err := r.Restore(t.Context(), RestoredWorker{ID: "original-worker", Role: "reviewer", Task: "continue the verified operation", Generation: 9, CloseRequested: closed}, r.ModelBuilder)
			if err != nil {
				t.Fatal(err)
			}
			if closed {
				waitSettlementDone(t, h)
				if len(r.PendingAgents()) != 0 || !r.SnapshotWorkers()[0].CloseRequested {
					t.Fatal("restored explicit close became unfinished new work")
				}
			} else {
				if got := r.PendingAgents(); len(got) != 1 || got[0].Generation != 9 || !got[0].Running {
					t.Fatalf("restored generation changed identity: %+v", got)
				}
				close(hold)
				waitSettlementDone(t, h)
			}
		})
	}
}

func waitSettlementDone(t *testing.T, h *Handle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("offline worker never reached its terminal receipt")
	}
}

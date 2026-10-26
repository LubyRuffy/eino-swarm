//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

type largeRosterCase struct {
	dir, data, probe       string
	fixture                *largeRosterFixture
	threads                map[string]store.Thread
	turns                  map[string]store.Turn
	history                map[string]bool
	wake                   store.Schedule
	old                    *restartProcess
	oldRecord              lease.Record
	children               []lease.Record
	stopped                map[int]bool
	beforeLive, beforePark int
	attempt                int
}

// The roster must be consumed by actual old registries before PrepareRestart.
// Merely appending a 351-element checkpoint cannot prove that boundary.
func TestReplacementRestoresLargeRosterWithLiveParkedAndHumanPause(t *testing.T) {
	c := newLargeRosterCase(t)
	largeFixtureRefusals(t, c.dir)
	started := time.Now()
	replacement := c.replace(t, "")
	listened := time.Since(started)
	c.waitRestored(t, replacement)
	c.freezeProbe(t, replacement.PID)
	c.assertCheckpoints(t, replacement)
	c.assertProbe(t, replacement.PID)
	c.finish(t, replacement)
	c.save(t, "successful-replacement.json", map[string]any{"old": c.oldRecord, "replacement": replacement, "handoff_listener_ms": listened.Milliseconds(), "restore_and_work_total_ms": time.Since(started).Milliseconds(), "historical_workers": 349, "actual_checkpoint_workers": 351, "explicitly_not_native_UI_acceptance": true})
}

// Cancelling after publication is an explicit human-equivalent cancellation,
// not permission for the obsolete 30-second deadline to kill the sole child.
func TestCancelledLargeRestoreReapsOwnedChildAndColdRecoversSameWork(t *testing.T) {
	c := newLargeRosterCase(t)
	started := time.Now()
	replacement := c.replace(t, c.threads["ask"].ID)
	waitRestartCondition(t, 15*time.Second, "real worker model input while actual Restore is blocked", func() bool {
		probe := c.readProbe(replacement.PID)
		c.fixture.mu.Lock()
		resumed := c.fixture.jobs["live"].worker > c.beforeLive && c.fixture.jobs["parked"].worker > c.beforePark && len(c.fixture.jobs["live"].unknownInputs) > 0
		c.fixture.mu.Unlock()
		return probe.BarrierEntered && resumed && largeStartup(t, replacement.URL) == "restoring"
	})
	var refused map[string]any
	restartJSON(t, "POST", replacement.URL+"/api/threads/"+c.threads["live"].ID+"/turns", map[string]string{"text": "This new work must be refused during restoration."}, http.StatusConflict, &refused)
	if refused["code"] != "restoring" {
		t.Fatalf("new work bypassed the actual restoring admission: %+v", refused)
	}
	c.save(t, "before-explicit-cancel.json", map[string]any{"replacement": replacement, "probe": c.readProbe(replacement.PID), "elapsed_ms": time.Since(started).Milliseconds(), "side_effect_before_ready": largeMarker(t, c.fixture.marker)})
	c.stop(t, replacement)
	partial := c.readProbe(replacement.PID)
	if !partial.BarrierCancelled || partial.ContextError == "" {
		t.Fatalf("actual Restore did not acknowledge cancellation: %+v", partial)
	}
	c.save(t, "cancelled-attempt.json", map[string]any{"process_exited": true, "interrupted_restore": true, "not_a_product_failure": true, "probe": partial, "checkpoint_reseeded": false})
	// Same durable directory, no synthetic checkpoint or rewritten state.
	t.Setenv("ZWAI_LARGE_RESTORE_BLOCK_THREAD", "")
	c.bindRequestBaseline(t)
	beforeCold := time.Now()
	recovered, err := ensureEngineRecord(c.data, "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	c.children = append(c.children, recovered)
	if recovered.PID == replacement.PID {
		t.Fatal("cold recovery reused the cancelled process identity")
	}
	c.waitRestored(t, recovered)
	c.freezeProbe(t, recovered.PID)
	c.assertCheckpoints(t, recovered)
	c.assertProbe(t, recovered.PID)
	c.finish(t, recovered)
	c.save(t, "cold-recovery.json", map[string]any{"cancelled": replacement, "cold": recovered, "cold_restore_and_work_ms": time.Since(beforeCold).Milliseconds(), "original_attempt_kept_separate": true, "checkpoint_reseeded": false})
}

func newLargeRosterCase(t *testing.T) *largeRosterCase {
	t.Helper()
	if testing.Short() {
		t.Skip("real engine replacement with 351 persisted workers")
	}
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	if root := os.Getenv("ZWAI_LARGE_ROSTER_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		var err error
		dir, err = os.MkdirTemp(root, t.Name()+"-")
		if err != nil {
			t.Fatal(err)
		}
	}
	c := &largeRosterCase{dir: dir, data: filepath.Join(dir, "data"), probe: filepath.Join(dir, "probe"), threads: map[string]store.Thread{}, turns: map[string]store.Turn{}, history: map[string]bool{}, stopped: map[int]bool{}}
	c.fixture = newLargeRosterFixture(t, filepath.Join(dir, "effect.txt"))
	t.Cleanup(func() {
		for _, rec := range c.children {
			if !c.stopped[rec.PID] && syscall.Kill(rec.PID, 0) == nil {
				current, err := lease.Discover(c.data, false)
				if err != nil || current.PID != rec.PID || !current.StartedAt.Equal(rec.StartedAt) || current.Dev != rec.Dev || current.Ino != rec.Ino {
					t.Errorf("cleanup refused an unverified old/reused process pid=%d error=%v", rec.PID, err)
					continue
				}
				if err := lease.Kill(rec.PID); err != nil {
					t.Error(err)
				}
			}
		}
		c.fixture.save(t, filepath.Join(c.dir, "actual-model-inputs.json"))
		if raw, _ := os.ReadFile(filepath.Join(c.data, "engine.log")); strings.Contains(string(raw), "WARNING: DATA RACE") {
			t.Error("replacement engine reported a data race")
		}
	})
	c.seed(t)
	t.Setenv("ZWAI_ENGINE_RESTORE_HELPER", "1")
	t.Setenv("ZWAI_RESTORE_OLD", "1")
	t.Setenv("ZWAI_LARGE_RESTORE_PROBE", "")
	t.Setenv("ZWAI_LARGE_RESTORE_BLOCK_THREAD", "")
	c.old = startRestartProcess(t, dir, os.Args[0], "engine", "--data-dir", c.data, "--addr", "127.0.0.1:0")
	c.oldRecord = waitRestartRecord(t, c.data, 0, 15*time.Second, c.old)
	if c.oldRecord.PID != c.old.cmd.Process.Pid {
		t.Fatal("private old lease belongs to another process")
	}
	waitRestartCondition(t, 10*time.Second, "old real engine readiness", func() bool { return largeStartup(t, c.oldRecord.URL) == "ready" })
	var armed struct {
		Schedule store.Schedule `json:"schedule"`
	}
	restartJSON(t, "POST", c.oldRecord.URL+"/api/schedules", map[string]any{"kind": "thread", "thread_id": c.threads["parked"].ID, "title": "Large roster retained work", "prompt": "Inspect the pending original work", "every_s": 3600}, http.StatusCreated, &armed)
	c.wake = armed.Schedule
	if c.wake.CreatedBy != store.ScheduleCreatedHuman || c.wake.NextRunAt.Before(time.Now().Add(3500*time.Second)) || c.wake.TitleAuto {
		t.Fatalf("fixture lacks an actual future human-owned wake: %+v", c.wake)
	}
	for _, kind := range []string{"live", "parked", "ask"} {
		prompt := map[string]string{"live": largeLivePrompt, "parked": largeParkPrompt, "ask": largeAskPrompt}[kind]
		var started struct {
			Turn store.Turn `json:"turn"`
		}
		restartJSON(t, "POST", c.oldRecord.URL+"/api/threads/"+c.threads[kind].ID+"/turns", map[string]string{"text": prompt}, http.StatusAccepted, &started)
		c.turns[kind] = started.Turn
	}
	waitRestartCondition(t, 15*time.Second, "live effect, actual parked worker and human pause", func() bool {
		live := largeStatus(t, c.oldRecord.URL, c.threads["live"].ID)
		parked := largeStatus(t, c.oldRecord.URL, c.threads["parked"].ID)
		ask := largeStatus(t, c.oldRecord.URL, c.threads["ask"].ID)
		return largeMarker(t, c.fixture.marker) == "recorded\n" && live.Running && !parked.Running && parked.Workers == 1 && ask.AwaitingAnswer
	})
	c.save(t, "before-handoff.json", map[string]any{"threads": c.threads, "turns": c.turns, "historical_ids": c.history, "wake": c.wake, "engine": c.oldRecord})
	return c
}

func (c *largeRosterCase) seed(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(c.data)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = []config.Provider{{ID: "large-roster", BaseURL: c.fixture.server.URL, Model: "fixture-model", TimeoutSeconds: 180, ContextWindow: 128000}}
	cfg.Models.Default = "large-roster"
	cfg.Swarm.AutoTitle, cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false, false
	cfg.Swarm.AgentTimeoutSeconds = 180
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for rank, kind := range []string{"live", "parked", "ask"} {
		th := store.Thread{Title: "Large roster " + kind, SortRank: rank + 1}
		if err := st.CreateThread(&th); err != nil {
			t.Fatal(err)
		}
		c.threads[kind] = th
		turn := store.Turn{ThreadID: th.ID, UserText: "Historical completed worker results"}
		if err := st.CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
		n := 116
		if rank == 0 {
			n = 117
		}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("historical-%s-%03d", kind, i)
			c.history[id] = true
			// Legacy generation zero is finished; it does not pretend that a
			// new-generation result has been collected by a current manager.
			for _, ev := range []store.Event{{ThreadID: th.ID, TurnID: turn.ID, AgentID: id, Role: "historical", Kind: swarm.NotifySpawned.String(), Text: "Historical assigned work"}, {ThreadID: th.ID, TurnID: turn.ID, AgentID: id, Role: "historical", Kind: swarm.NotifyFinished.String(), Text: "Historical confirmed result " + id}} {
				if err := st.AppendEvent(&ev); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := st.FinishTurn(turn.ID, store.TurnDone, "Historical work retained", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func (c *largeRosterCase) replace(t *testing.T, block string) lease.Record {
	t.Helper()
	t.Setenv("ZWAI_RESTORE_OLD", "")
	t.Setenv("ZWAI_LARGE_RESTORE_PROBE", c.probe)
	t.Setenv("ZWAI_LARGE_RESTORE_BLOCK_THREAD", block)
	previous := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = previous })
	c.bindRequestBaseline(t)
	started := time.Now()
	rec, err := ensureEngineRecord(c.data, "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	c.children = append(c.children, rec)
	var meta struct {
		DataDir string `json:"data_dir"`
	}
	restartJSON(t, "GET", rec.URL+"/api/meta", nil, http.StatusOK, &meta)
	if meta.DataDir != c.data {
		t.Fatalf("replacement changed private data: got %q want %q", meta.DataDir, c.data)
	}
	if time.Since(started) >= 30*time.Second || rec.PID == c.oldRecord.PID || rec.Version == c.oldRecord.Version {
		t.Fatalf("real replacement missed handoff/listener or kept old identity: old=%+v new=%+v elapsed=%s", c.oldRecord, rec, time.Since(started))
	}
	select {
	case <-c.old.done:
		if c.old.waitErr != nil {
			t.Fatalf("checkpointed old process failed: %v\n%s", c.old.waitErr, c.old.output())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old engine did not exit after actual replacement")
	}
	return rec
}

func (c *largeRosterCase) bindRequestBaseline(t *testing.T) {
	c.fixture.mu.Lock()
	c.beforeLive = c.fixture.jobs["live"].worker
	c.beforePark = c.fixture.jobs["parked"].worker
	c.fixture.mu.Unlock()
	c.attempt++
	c.save(t, fmt.Sprintf("worker-request-baseline-%d.json", c.attempt), map[string]any{"attempt": c.attempt, "at": time.Now().UTC(), "live": c.beforeLive, "parked": c.beforePark, "before_new_engine_launch": true})
}

type largeThreadStatus struct {
	Running        bool   `json:"running"`
	AwaitingAnswer bool   `json:"awaiting_answer"`
	Workers        int    `json:"workers"`
	TurnID         string `json:"turn_id"`
}

func largeStatus(t *testing.T, base, thread string) largeThreadStatus {
	t.Helper()
	var response struct {
		Status largeThreadStatus `json:"status"`
	}
	restartJSON(t, "GET", base+"/api/threads/"+thread, nil, 200, &response)
	return response.Status
}

func largeStartup(t *testing.T, base string) string {
	t.Helper()
	var meta struct {
		Startup string `json:"startup"`
	}
	restartJSON(t, "GET", base+"/api/meta", nil, 200, &meta)
	return meta.Startup
}

func largeMarker(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}

func (c *largeRosterCase) save(t *testing.T, name string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.dir, name), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (c *largeRosterCase) readProbe(pid int) largeRestoreReceipt {
	raw, _ := os.ReadFile(filepath.Join(c.probe, fmt.Sprintf("%d.json", pid)))
	var receipt largeRestoreReceipt
	_ = json.Unmarshal(raw, &receipt)
	return receipt
}

func (c *largeRosterCase) waitRestored(t *testing.T, rec lease.Record) {
	t.Helper()
	waitRestartCondition(t, 25*time.Second, "same two original worker requests and recovered human pause", func() bool {
		p := c.readProbe(rec.PID)
		c.fixture.mu.Lock()
		resumed := c.fixture.jobs["live"].worker > c.beforeLive && c.fixture.jobs["parked"].worker > c.beforePark
		c.fixture.mu.Unlock()
		ask := p.Threads[c.threads["ask"].ID]
		if !resumed || p.Phase != "restore_returned" || ask == nil || !ask.AskRecovered || largeStartup(t, rec.URL) != "ready" {
			return false
		}
		// Listing reads only thread rows/runtime status. The single-thread
		// endpoint also measures context usage and would pollute this probe.
		var rows struct {
			Threads []struct {
				ID             string `json:"id"`
				AwaitingAnswer bool   `json:"awaiting_answer"`
			} `json:"threads"`
		}
		restartJSON(t, "GET", rec.URL+"/api/threads", nil, http.StatusOK, &rows)
		for _, row := range rows.Threads {
			if row.ID == c.threads["ask"].ID {
				return row.AwaitingAnswer
			}
		}
		return false
	})
	c.fixture.mu.Lock()
	live, parked := c.fixture.jobs["live"].worker, c.fixture.jobs["parked"].worker
	c.fixture.mu.Unlock()
	c.save(t, fmt.Sprintf("actual-model-observation-%d.json", rec.PID), map[string]any{"engine": rec, "attempt": c.attempt, "at": time.Now().UTC(), "baseline_live": c.beforeLive, "baseline_parked": c.beforePark, "observed_live": live, "observed_parked": parked, "new_actual_requests_both": live > c.beforeLive && parked > c.beforePark, "actual_ask_waiter_armed": true, "before_probe_freeze": true})
}

func (c *largeRosterCase) freezeProbe(t *testing.T, pid int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(c.probe, fmt.Sprintf("freeze-%d", pid)), []byte("freeze actual recovery counts before observers\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitRestartCondition(t, 5*time.Second, "actual DB counter freeze acknowledgement", func() bool { return c.readProbe(pid).Frozen })
}

func (c *largeRosterCase) assertProbe(t *testing.T, pid int) {
	t.Helper()
	p := c.readProbe(pid)
	if p.ContextError != "" || p.UnattributedReads != 0 || !p.Frozen {
		t.Fatalf("incomplete actual restore accounting: %+v", p)
	}
	for kind, th := range c.threads {
		counts := p.Threads[th.ID]
		if counts == nil || counts.CheckpointReads != 1 || counts.FilteredReads != 1 || counts.ThreadBodyReads != 0 || counts.ScannedEvents == 0 {
			t.Errorf("recovery repeated/omitted the actual snapshot for %s: %+v", th.ID, counts)
			continue
		}
		if counts.FullReads != 0 || counts.TurnBodyReads != 0 {
			// Retain SQL/body size/callers without exempting human-pause
			// bootstrap from C-027's one immutable recovery snapshot.
			t.Errorf("%s bootstrap rescanned history: thread=%s counts=%+v", kind, th.ID, counts)
		}
	}
	c.save(t, fmt.Sprintf("frozen-restore-probe-%d.json", pid), p)
}

func (c *largeRosterCase) assertCheckpoints(t *testing.T, rec lease.Record) {
	t.Helper()
	seen, finished := map[string]bool{}, map[string]bool{}
	for kind, turn := range c.turns {
		trace := restartTrace(t, rec.URL, turn.ID)
		var cp struct {
			Version        int                      `json:"version"`
			Pause          string                   `json:"pause"`
			AskCallID      string                   `json:"ask_call_id"`
			ManagerHistory []*schema.Message        `json:"manager_history"`
			Workers        []swarm.WorkerCheckpoint `json:"workers"`
		}
		lastCheckpoint := ""
		for _, event := range trace.Events {
			if event.Kind == engine.KindRestartCheckpoint {
				lastCheckpoint = event.Text
			}
		}
		if err := json.Unmarshal([]byte(lastCheckpoint), &cp); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"live": 118, "parked": 117, "ask": 116}[kind]
		if cp.Version != 1 || len(cp.Workers) != want || kind == "ask" && (cp.Pause != "ask" || cp.AskCallID != "" && cp.AskCallID != "large-original-ask") {
			t.Fatalf("actual latest PrepareRestart checkpoint lost %s registry: version=%d workers=%d pause=%q optional_call=%q", kind, cp.Version, len(cp.Workers), cp.Pause, cp.AskCallID)
		}
		if kind == "ask" {
			calls := 0
			for _, message := range cp.ManagerHistory {
				if message != nil {
					for _, call := range message.ToolCalls {
						if call.Function.Name == engine.ToolAskUser {
							if call.ID != "large-original-ask" || !largeAskArgumentsMatch(call.Function.Arguments) {
								t.Fatalf("checkpoint replaced original ask call: %s", call.ID)
							}
							calls++
						}
					}
				}
			}
			if calls != 1 {
				t.Fatalf("actual checkpoint history lost original human call: %d", calls)
			}
			traceCalls := 0
			for _, event := range trace.Events {
				if event.Kind != swarm.NotifyToolCall.String() || event.ToolCallID != "large-original-ask" {
					continue
				}
				prefix := engine.ToolAskUser + "("
				if !strings.HasPrefix(event.Text, prefix) || !strings.HasSuffix(event.Text, ")") || !largeAskArgumentsMatch(strings.TrimSuffix(strings.TrimPrefix(event.Text, prefix), ")")) {
					t.Fatalf("original Trace changed human question: %s", event.Text)
				}
				traceCalls++
			}
			if traceCalls != 1 {
				t.Fatalf("original Trace lost or repeated the human ask call: %d", traceCalls)
			}
		}
		for _, worker := range cp.Workers {
			if seen[worker.ID] {
				t.Fatalf("duplicate worker across checkpoints: %s", worker.ID)
			}
			seen[worker.ID] = true
			if worker.Finished {
				finished[worker.ID] = true
				// Actual PlantFinished normalizes legacy generation zero to
				// generation one + collected; this is not a new execution.
				if !c.history[worker.ID] || worker.Generation != 1 || !worker.Collected || worker.Err != "" || worker.Result != "Historical confirmed result "+worker.ID {
					t.Fatalf("historical result/legacy normalization changed in actual registry checkpoint: id=%s generation=%d collected=%v", worker.ID, worker.Generation, worker.Collected)
				}
			} else if worker.Generation != 1 {
				t.Errorf("original live generation changed: id=%s generation=%d", worker.ID, worker.Generation)
			}
		}
		c.save(t, "actual-handoff-checkpoint-"+kind+".json", cp)
	}
	if len(seen) != 351 || !reflect.DeepEqual(finished, c.history) {
		t.Fatalf("actual handoff roster changed: total=%d finished=%d historical=%d", len(seen), len(finished), len(c.history))
	}
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()
	for _, kind := range []string{"live", "parked"} {
		id := c.fixture.jobs[kind].workerID
		if id == "" || !seen[id] || finished[id] || c.fixture.jobs[kind].spawn != 1 {
			t.Errorf("actual original %s worker missing or repeated: %q", kind, id)
		}
	}
}

func (c *largeRosterCase) finish(t *testing.T, rec lease.Record) {
	t.Helper()
	askThread := c.threads["ask"].ID
	if !largeStatus(t, rec.URL, askThread).AwaitingAnswer {
		t.Fatal("restart bypassed the human pause")
	}
	c.fixture.mu.Lock()
	beforeAnswer := c.fixture.jobs["ask"].manager
	c.fixture.mu.Unlock()
	if beforeAnswer != 1 {
		t.Fatal("manager model ran before the original answer")
	}
	restartJSON(t, "POST", rec.URL+"/api/threads/"+askThread+"/answers", map[string]any{"call_id": "large-original-ask", "answers": engine.AskAnswers{"direction": {Answers: []string{"First"}}}}, 202, nil)
	c.fixture.release()
	for kind, turn := range c.turns {
		waitRestartCondition(t, 15*time.Second, "original "+kind+" task and worker completion", func() bool {
			st := largeStatus(t, rec.URL, c.threads[kind].ID)
			return !st.Running && st.Workers == 0 && restartTrace(t, rec.URL, turn.ID).Turn.Status == store.TurnDone
		})
		trace := restartTrace(t, rec.URL, turn.ID)
		if trace.Turn.ID != turn.ID || trace.Turn.Error != "" {
			t.Fatalf("original task failed or changed identity: %+v", trace.Turn)
		}
		var turns struct {
			Turns []store.Turn `json:"turns"`
		}
		restartJSON(t, "GET", rec.URL+"/api/threads/"+c.threads[kind].ID+"/turns", nil, http.StatusOK, &turns)
		if len(turns.Turns) != 2 || turns.Turns[1].ID != turn.ID {
			t.Fatalf("recovery created or replaced the original %s turn: %+v", kind, turns.Turns)
		}
		if kind != "ask" {
			c.fixture.mu.Lock()
			id := c.fixture.jobs[kind].workerID
			c.fixture.mu.Unlock()
			spawned, generations, completed, spawnCalls := 0, 0, 0, 0
			for _, event := range trace.Events {
				if event.Kind == swarm.NotifySpawned.String() && event.AgentID != swarm.DefaultManagerID && event.AgentID != id {
					t.Errorf("recovery created another worker identity: %s", event.AgentID)
				}
				if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == "large-spawn-"+kind {
					spawnCalls++
				}
				if event.AgentID != id {
					continue
				}
				if event.Kind == swarm.NotifySpawned.String() {
					spawned++
				}
				if event.Kind == "generation_started" {
					generations++
				}
				if event.Kind == swarm.NotifyFinished.String() && event.Err == "" {
					completed++
				}
			}
			// Restore emits spawned again for the SAME worker id. Count the
			// real manager tool call separately from resumed notifications.
			if spawned < 2 || spawnCalls != 1 || generations < 2 || completed != 1 {
				t.Errorf("original %s worker execution/finish identity changed: id=%s spawn_calls=%d spawn_or_restore_notifications=%d generations=%d completed=%d", kind, id, spawnCalls, spawned, generations, completed)
			}
		}
		c.save(t, "final-trace-"+kind+".json", trace)
	}
	restartJSON(t, "POST", rec.URL+"/api/threads/"+askThread+"/answers", map[string]any{"call_id": "large-original-ask", "answers": engine.AskAnswers{"direction": {Answers: []string{"Second"}}}}, 409, nil)
	var schedule struct {
		Schedule store.Schedule `json:"schedule"`
	}
	restartJSON(t, "GET", rec.URL+"/api/schedules/"+c.wake.ID, nil, 200, &schedule)
	if !reflect.DeepEqual(c.wake, schedule.Schedule) {
		t.Fatalf("recovery fired or changed the human future wake: before=%+v after=%+v", c.wake, schedule.Schedule)
	}
	if largeMarker(t, c.fixture.marker) != "recorded\n" {
		t.Fatal("unknown side effect was repeated or lost")
	}
	c.fixture.mu.Lock()
	if len(c.fixture.errors) > 0 || c.fixture.jobs["live"].verify != 1 || len(c.fixture.jobs["live"].unknownInputs) == 0 || c.fixture.jobs["ask"].manager != 2 {
		t.Errorf("fixture lost original work/unknown/human answer: errors=%v verify=%d unknown_inputs=%d ask_requests=%d", c.fixture.errors, c.fixture.jobs["live"].verify, len(c.fixture.jobs["live"].unknownInputs), c.fixture.jobs["ask"].manager)
	}
	c.fixture.mu.Unlock()
	c.stop(t, rec)
}

func (c *largeRosterCase) stop(t *testing.T, rec lease.Record) {
	t.Helper()
	current, err := lease.Discover(c.data, false)
	if err != nil || current.PID != rec.PID || current.StartedAt != rec.StartedAt || current.Dev != rec.Dev || current.Ino != rec.Ino {
		t.Fatalf("refuse to signal a process without the original private lease: %+v %v", current, err)
	}
	if err := lease.Stop(rec.PID); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(rec.PID, 0); err != syscall.ESRCH {
		t.Fatalf("original child not reaped: pid=%d err=%v", rec.PID, err)
	}
	c.stopped[rec.PID] = true
	hold, err := lease.Acquire(c.data)
	if err != nil {
		t.Fatalf("original child retained its private data lock: %v", err)
	}
	hold.Release()
	c.save(t, fmt.Sprintf("child-%d-cleanup.json", rec.PID), map[string]any{"original_lease": rec, "SIGTERM_requested": true, "fresh_pid_ESRCH": true, "private_lease_reacquired_and_closed": true, "spawnEngineContext_original_Wait_goroutine_reaped_child": true})
}

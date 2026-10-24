package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// The real resumed bootstrap runs after Restore returns. Its ask, tool and
// generation helpers must consume that same snapshot instead of rereading MBs.
func TestRestoreSharesOneCheckpointAndEventReadWithResumedBootstrap(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
	cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}, Workers: []swarm.WorkerCheckpoint{{ID: "existing-worker", Role: "inspector", Finished: true, Result: "confirmed"}}}
	body, _ := json.Marshal(cp)
	for _, ev := range []store.Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: turn.UserText},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: strings.Repeat("x", 1<<20)},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "agent_delta", Text: strings.Repeat("y", 1<<20)},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "generation_started", MessageID: "old-generation", AgentID: "manager"},
	} {
		if err := e.store.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	checkpointReads, eventReads, fullReads := 0, 0, 0
	bootstrapDone := false
	createdCallback := "mark_restore_bootstrap_done"
	if err := e.store.DB().Callback().Create().After("gorm:create").Register(createdCallback, func(db *gorm.DB) {
		if ev, ok := db.Statement.Dest.(*store.Event); ok && ev.Kind == "generation_started" {
			mu.Lock()
			bootstrapDone = true
			mu.Unlock()
		}
	}); err != nil {
		t.Fatal(err)
	}
	// This database is private to the test. Keep callbacks until Store.Close:
	// GORM callback removal races the resumed turn's finalization queries.
	callback := "count_restore_reads"
	countRead := func(db *gorm.DB) {
		if db.Statement.Table != "events" {
			return
		}
		sql := db.Statement.SQL.String()
		mu.Lock()
		defer mu.Unlock()
		if bootstrapDone {
			return
		}
		if strings.Contains(sql, "kind =") && strings.Contains(sql, "ORDER BY seq desc") {
			checkpointReads++
		}
		if strings.Contains(sql, "AND (kind IN") {
			eventReads++
		}
		if !strings.Contains(sql, "kind") && (strings.Contains(sql, "seq >") || strings.Contains(sql, "turn_id =")) {
			fullReads++
		}
	}
	if err := e.store.DB().Callback().Query().After("gorm:query").Register(callback, countRead); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Callback().Row().After("gorm:row").Register(callback, countRead); err != nil {
		t.Fatal(err)
	}
	defer e.Shutdown()
	var progress [][2]int
	if err := e.Restore(context.Background(), func(done, total int) { progress = append(progress, [2]int{done, total}) }); err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("turn=%+v", finished)
	}
	mu.Lock()
	gotCheckpoint, gotEvents, gotFull := checkpointReads, eventReads, fullReads
	mu.Unlock()
	if gotCheckpoint != 1 || gotEvents != 1 || gotFull != 0 {
		t.Fatalf("checkpoint reads=%d filtered reads=%d full scans=%d", gotCheckpoint, gotEvents, gotFull)
	}
	if len(progress) != 2 || progress[0] != [2]int{0, 1} || progress[1] != [2]int{1, 1} {
		t.Fatalf("progress=%v", progress)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	if countKind(events, "engine_restore") != 1 || countKind(events, "spawned") != 0 {
		t.Fatalf("restore receipt or roster identity invalid: %+v", events)
	}
	for _, ev := range events {
		if ev.Kind == "engine_restore" {
			var receipt map[string]any
			if json.Unmarshal([]byte(ev.Text), &receipt) != nil || receipt["scanned_events"] != float64(3) {
				t.Fatalf("receipt=%s", ev.Text)
			}
		}
	}
}

func TestRestoreCancellationKeepsDurableUnfinishedWork(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "unfinished")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Restore(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	got, _ := e.store.GetTurn(turn.ID)
	if got.Status != store.TurnRunning || e.Status(th.ID).Running {
		t.Fatalf("cancelled restore mutated work: %+v", got)
	}
}

func TestRestoreCancelledAfterSnapshotRetainsItsCheckpointAndCause(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "unfinished")
	appendParkedCheckpoint(t, e, th.ID, turn.ID, restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback := "cancel_after_recovery_snapshot"
	if err := e.store.DB().Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "events" && strings.Contains(db.Statement.SQL.String(), "AND (kind IN") {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	got, _ := e.store.GetTurn(turn.ID)
	if got.Status != store.TurnRunning || e.Status(th.ID).Running {
		t.Fatalf("cancelled restore terminalized work: %+v", got)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	found := false
	for _, ev := range events {
		if ev.Kind == "engine_restore" {
			found = true
			var receipt map[string]any
			if err := json.Unmarshal([]byte(ev.Text), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt["phase"] != "cancelled" || receipt["error_category"] != "context_cancelled" {
				t.Fatalf("cancellation receipt=%s", ev.Text)
			}
		}
	}
	if !found || e.checkpointForTurn(turn.ID) == nil {
		t.Fatal("cancelled restoration lost checkpoint or trace")
	}
}

func TestRestoreKeepsParkedTurnAndReportsRealRecoveryFailure(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "failed"}[broken], func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, wake := plantParkedRestart(t, e)
			if broken {
				if err := e.store.DB().Model(&store.Turn{}).Where("id = ?", turn.ID).Update("provider_id", "unconfigured-provider").Error; err != nil {
					t.Fatal(err)
				}
			}
			err := e.Restore(context.Background(), nil)
			if broken {
				var failures *RestoreFailures
				if !errors.As(err, &failures) || failures.Failed != 1 || !strings.Contains(failures.Error(), "1 conversation(s)") {
					t.Fatalf("missing typed recovery failures: %v", err)
				}
			}
			if broken != (err != nil) {
				t.Fatalf("broken=%v err=%v", broken, err)
			}
			got, _ := e.store.GetTurn(turn.ID)
			if got.Status != store.TurnDone || got.Final != "waiting for the next check" {
				t.Fatalf("changed parked manager=%+v", got)
			}
			schedule, _ := e.store.GetSchedule(wake.ID)
			if !schedule.NextRunAt.Equal(wake.NextRunAt) {
				t.Fatal("restoration fired future manager wake")
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			for _, ev := range events {
				if ev.Kind == "engine_restore" {
					var receipt map[string]any
					if err := json.Unmarshal([]byte(ev.Text), &receipt); err != nil {
						t.Fatal(err)
					}
					want := "restored"
					if broken {
						want = "failed"
					}
					if receipt["phase"] != want {
						t.Fatalf("receipt=%s", ev.Text)
					}
				}
			}
			if !broken {
				waitRestartState(t, func() bool { return e.Status(th.ID).Workers == 0 })
			}
		})
	}
}

func TestRestoreHandlesEmptyThreadsAndSupersededRunningRows(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.CreateThread("empty", "", ""); err != nil {
		t.Fatal(err)
	}
	th, _ := e.CreateThread("running", "", "")
	old := plantUnfinishedTurn(t, e, th.ID, "old")
	latest := plantUnfinishedTurn(t, e, th.ID, "latest")
	if err := e.Restore(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetTurn(old.ID)
	if got.Status != store.TurnCancelled {
		t.Fatalf("superseded turn=%+v", got)
	}
	if got := waitForTurn(t, e, latest.ID); got.Status != store.TurnDone {
		t.Fatalf("latest=%+v", got)
	}
}

func TestRestoreReportsReadFailuresAndRetainsUnstartedTurns(t *testing.T) {
	for _, boundary := range []string{"threads", "schedules", "turns", "checkpoint", "events", "cancel-after-turns", "cancel-progress"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "unfinished")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("injected recovery read failure")
			callback := "fail_restore_" + boundary
			faultInjected := false
			failRead := func(db *gorm.DB) {
				if faultInjected {
					return
				}
				table, sql := db.Statement.Table, db.Statement.SQL.String()
				matches := boundary == table && boundary != "events"
				matches = matches || boundary == "checkpoint" && table == "events" && strings.Contains(sql, "kind =")
				matches = matches || boundary == "events" && table == "events" && strings.Contains(sql, "AND (kind IN")
				if matches {
					faultInjected = true
					_ = db.AddError(failure)
				}
			}
			if err := e.store.DB().Callback().Query().After("gorm:query").Register(callback, failRead); err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Callback().Row().After("gorm:row").Register(callback, failRead); err != nil {
				t.Fatal(err)
			}

			if boundary == "cancel-after-turns" {
				if err := e.store.DB().Callback().Query().After("gorm:query").Register("cancel_turn_read", func(db *gorm.DB) {
					if db.Statement.Table == "turns" {
						cancel()
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			var progress func(int, int)
			if boundary == "cancel-progress" {
				progress = func(int, int) { cancel() }
			}
			err := e.Restore(ctx, progress)
			if boundary == "cancel-after-turns" || boundary == "cancel-progress" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation=%v", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("read failure=%v", err)
			}
			got, _ := e.store.GetTurn(turn.ID)
			if got == nil || got.Status != store.TurnRunning || e.Status(th.ID).Running {
				t.Fatalf("failed restoration changed work: %+v", got)
			}
		})
	}
}

func TestRestorePullsOnlyTheInterruptedWorkersFutureWake(t *testing.T) {
	for _, failUpdate := range []bool{false, true} {
		t.Run(map[bool]string{false: "advance", true: "persistence-failure"}[failUpdate], func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "original")
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "waiting", ""); err != nil {
				t.Fatal(err)
			}
			wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Prompt: "check existing work", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager})
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range []store.Event{
				{ThreadID: th.ID, TurnID: turn.ID, Kind: "spawned", AgentID: "same-worker", Role: "inspector"},
				{ThreadID: th.ID, TurnID: turn.ID, Kind: "finished", AgentID: "same-worker", Err: "context canceled"},
				{ThreadID: th.ID, TurnID: turn.ID, Kind: "generation_started", AgentID: "same-worker", MessageID: "old-output"},
			} {
				if err := e.store.AppendEvent(&ev); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("injected schedule persistence failure")
			if failUpdate {
				if err := e.store.DB().Callback().Update().Before("gorm:update").Register("fail_restored_wake", func(db *gorm.DB) {
					if db.Statement.Table == "schedules" {
						_ = db.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			err = e.Restore(context.Background(), nil)
			if failUpdate {
				if !errors.Is(err, failure) {
					t.Fatalf("update failure=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, _ := e.store.GetSchedule(wake.ID)
			if got.NextRunAt.Equal(wake.NextRunAt) != failUpdate {
				t.Fatalf("wake result=%+v", got)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			found := false
			for _, ev := range events {
				if ev.Kind == "generation_interrupted" && ev.MessageID == "old-output" {
					found = true
					var body swarm.GenerationInterruption
					if err := json.Unmarshal([]byte(ev.Text), &body); err != nil {
						t.Fatal(err)
					}
					if body.Recovery != "none" || body.Reason != "process_exit" {
						t.Fatalf("generation=%+v", body)
					}
				}
			}
			if !found {
				t.Fatal("terminal conversation left an old thinking stream open")
			}
		})
	}
}

func TestRestoreDoesNotPullAnotherConversationsWake(t *testing.T) {
	e := newTestEngine(t)
	th, _, wake := plantParkedRestart(t, e)
	snapshot := &restoreSnapshot{events: []store.Event{{Kind: "spawned", TurnID: "original", AgentID: "worker"}, {Kind: "finished", TurnID: "original", AgentID: "worker", Err: "context canceled"}}}
	foreign := *wake
	foreign.ThreadID = "another-conversation"
	if err := e.pullInterruptedThreadWake(th.ID, snapshot, []store.Schedule{foreign}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetSchedule(wake.ID)
	if !got.NextRunAt.Equal(wake.NextRunAt) {
		t.Fatal("a foreign row changed this conversation's wake")
	}
}

func TestRestoreFallsBackPastSchemaInvalidCheckpointWithoutLosingWorkers(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
	appendParkedCheckpoint(t, e, th.ID, turn.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{ID: "retained-worker", Role: "inspector", Instruction: "verify unfinished work"}}})
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: `{"version":1,"workers":"invalid-schema"}`}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := e.loadRestoreSnapshot(context.Background(), th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.checkpoint == nil || len(snapshot.running) != 1 || snapshot.running[0].ID != "retained-worker" {
		t.Fatalf("schema-invalid checkpoint concealed the earlier durable workers: %+v", snapshot.running)
	}
}

func TestRestoreKeepsPlanModeAndExposesAnUnconfiguredModel(t *testing.T) {
	for _, mode := range []string{"plan", "unconfigured"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
			if mode == "plan" {
				if err := e.store.UpdateThread(th.ID, map[string]any{"plan_mode": true}); err != nil {
					t.Fatal(err)
				}
			} else {
				e.pool = provider.New(e.cfg)
			}
			err := e.resumeTurn(context.Background(), turn, nil)
			if mode == "unconfigured" {
				if err == nil {
					t.Fatal("a missing model configuration was silently accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("plan-mode recovery=%+v", got)
			}
		})
	}
}

func TestOrphanedAskRecoveryKeepsUnusableInputOutOfTheWaiter(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
	messages := []adk.Message{schema.UserMessage("retained input")}
	if got, err := (*runtime)(nil).resolveOrphanedAsk(context.Background(), turn, messages); err != nil || len(got) != 1 {
		t.Fatalf("nil recovery changed input: messages=%v err=%v", got, err)
	}
	rt := e.runtimeFor(th.ID)
	snapshot := &restoreSnapshot{events: []store.Event{{ThreadID: th.ID, TurnID: turn.ID, Kind: "tool_call", ToolCallID: "invalid-ask", Text: "ask_user(invalid-json)"}}}
	if got, err := rt.resolveOrphanedAsk(context.Background(), turn, messages, snapshot); err != nil || len(got) != 1 {
		t.Fatalf("invalid question changed retained input: messages=%v err=%v", got, err)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	if len(events) != 1 || events[0].Kind != "tool_result" || events[0].ToolCallID != "invalid-ask" || events[0].Err != resumeToolStopped {
		t.Fatalf("invalid question remained pending: %+v", events)
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := rt.resolveOrphanedAsk(context.Background(), turn, messages); err == nil || len(got) != 1 {
		t.Fatalf("unreadable recovery must retain input and report its error: messages=%v err=%v", got, err)
	}
}

func TestSteerRecoveryUsesSnapshotRetractionsAndConsumedIdentity(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
	snapshot := &restoreSnapshot{events: []store.Event{
		{TurnID: turn.ID, Seq: 1, Kind: KindSteer, Text: "retracted"},
		{TurnID: turn.ID, Seq: 2, Kind: KindSteerRetracted, Text: `{"seq":1}`},
		{TurnID: turn.ID, Seq: 3, Kind: KindSteer, Text: "consumed"},
		{TurnID: turn.ID, Seq: 4, Kind: "agent_message", AgentID: swarm.DefaultManagerID, Text: "already acted"},
		{TurnID: turn.ID, Seq: 5, Kind: KindSteer, Text: "not read yet"},
	}}
	messages := e.unreadSteerMessages(turn, snapshot)
	if len(messages) != 1 || swarm.SteerSeq(messages[0]) != 5 || messages[0].Content != "[steer] not read yet" {
		t.Fatalf("wrong steering identity=%+v", messages)
	}
	if got := (*Engine)(nil).unreadSteerMessages(turn); got != nil {
		t.Fatal("nil engine fabricated steering")
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if got := e.unreadSteerMessages(turn); got != nil {
		t.Fatal("unreadable steering was fabricated")
	}
}

func TestGenerationRecoveryNeedsACheckpointNewerThanThatOutput(t *testing.T) {
	e := newTestEngine(t)
	for _, later := range []bool{false, true} {
		seq := int64(1)
		if later {
			seq = 3
		}
		snapshot := &restoreSnapshot{checkpointEvent: &store.Event{Seq: seq}}
		got := e.generationExitCause(store.Event{Seq: 2}, "pending", true, snapshot)
		want := "process_exit"
		if later {
			want = "engine_restart"
		}
		if got.Reason != want || got.Recovery != "pending" {
			t.Fatalf("later=%v cause=%+v", later, got)
		}
	}
}

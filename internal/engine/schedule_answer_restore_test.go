package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestPendingAnswerRecoveryUsesOnlyTheLatestCandidateAndItsOriginalIdentity(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("private answer recovery", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	old := saveRestoreAnswer(t, e, turn, "older-answer", "answer", "pending", nil)
	latest := saveRestoreAnswer(t, e, turn, "latest-answer", "answer", "pending", nil)
	saveRestoreAnswer(t, e, turn, "newer-report", "report", "pending", nil)
	row, payload, failure := e.restorePendingScheduleAnswer(turn)
	if failure != nil || row == nil || row.ID != latest.ID || payload.AnswerID != latest.ID || payload.Final != "The inspected state is ready." || len(payload.Transcript) != 1 || payload.Transcript[0].Extra[pendingScheduleAnswerKey] != latest.ID {
		t.Fatalf("recovery changed the original pending output: row=%+v payload=%+v failure=%v", row, payload, failure)
	}
	if err := e.store.DB().Model(&store.PendingScheduleAnswer{}).Where("id = ?", latest.ID).Update("state", "accepted").Error; err != nil {
		t.Fatal(err)
	}
	row, payload, failure = e.restorePendingScheduleAnswer(turn)
	if failure != nil || row == nil || row.State != "accepted" || payload.AnswerID != latest.ID {
		t.Fatalf("accepted candidate must resume without another publication: %+v %+v %v", row, payload, failure)
	}
	if err := e.store.RejectPendingScheduleAnswer(th.ID, turn.ID, latest.ID); err == nil {
		// Accepted records cannot transition backwards; rejection below models
		// the latest rejected answer independently of acceptance's transaction.
		t.Fatal("accepted candidate became rejected")
	}
	rejected := saveRestoreAnswer(t, e, turn, "rejected-answer", "answer", "pending", nil)
	if err := e.store.RejectPendingScheduleAnswer(th.ID, turn.ID, rejected.ID); err != nil {
		t.Fatal(err)
	}
	row, payload, failure = e.restorePendingScheduleAnswer(turn)
	if failure != nil || row != nil || payload != nil {
		t.Fatalf("rejected latest candidate revived an older answer %s: %+v %+v %v", old.ID, row, payload, failure)
	}
}

func TestPendingAnswerRecoveryRejectsUnreadableOrMalformedPrivateState(t *testing.T) {
	for _, raw := range []string{`{`, `{"version":2,"answer_id":"broken"}`, `{"version":1,"answer_id":"other"}`, `{"version":1,"answer_id":"broken","unexpected":true}`, `{"version":1,"answer_id":"broken"} {}`, `{"version":1,"answer_id":"broken","events":[{"thread_id":"other","turn_id":"wrong","kind":"agent_message","agent_id":"manager"}]}`, `{"version":1,"answer_id":"broken","transcript":[{"role":"assistant","content":"pending","extra":{"zwai_pending_schedule_answer":"other"}}]}`} {
		t.Run(raw, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("invalid private answer", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			saveRestoreAnswer(t, e, turn, "broken", "answer", "pending", &raw)
			row, payload, failure := e.restorePendingScheduleAnswer(turn)
			if row != nil || payload != nil || failure == nil || failure.Code != "answer_restore_invalid" {
				t.Fatalf("invalid candidate became executable: %+v %+v %v", row, payload, failure)
			}
		})
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("missing private store", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if err := e.store.DB().Migrator().DropTable(&store.PendingScheduleAnswer{}); err != nil {
		t.Fatal(err)
	}
	if row, payload, failure := e.restorePendingScheduleAnswer(turn); row != nil || payload != nil || failure == nil || failure.Code != "answer_restore_unavailable" {
		t.Fatalf("unreadable private state allowed execution: %+v %+v %v", row, payload, failure)
	}
}

func TestRecoveredReportReceiptSettlesTheOriginalCallBeforeUnknownCleanup(t *testing.T) {
	for _, state := range []string{"pending", "accepted", "rejected"} {
		t.Run(state, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("committed report recovery", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			call := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: ToolReportSchedule + `({"next_in_s":90,"findings":"The inspected state is ready."})`}
			if err := e.store.AppendEvent(&call); err != nil {
				t.Fatal(err)
			}
			row := saveRestoreAnswer(t, e, turn, "private-report", "report", state, nil)
			snapshot := &restoreSnapshot{threadID: th.ID, events: []store.Event{call}}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			if err := e.restorePendingScheduleReports(turn, snapshot); err != nil {
				t.Fatal(err)
			}
			if len(orphanedToolCalls(snapshot.events)) != 0 {
				t.Fatal("the immutable startup input still treats the committed report as unknown")
			}
			e.closeOrphanedToolCalls(turn, snapshot)
			if err := e.restorePendingScheduleReports(turn, snapshot); err != nil {
				t.Fatal(err)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			results := 0
			for _, event := range events {
				if event.Kind == swarm.NotifyToolResult.String() {
					results++
					if event.ToolCallID != "original-report" || event.Err != "" || !strings.Contains(event.Text, `"report_pending":true`) {
						t.Fatalf("the committed action lost its exact receipt: %+v", event)
					}
				}
				if event.Kind == KindScheduleReport {
					t.Fatal("repairing a tool receipt published unverified report findings")
				}
			}
			if results != 1 {
				t.Fatalf("recovery duplicated the committed report result: %d", results)
			}
			fresh, _ := e.store.GetPendingScheduleAnswer(row.ID)
			if fresh.State != state {
				t.Fatalf("tool receipt repair changed classification admission: %+v", fresh)
			}
			select {
			case event := <-sub.C:
				if event.Kind != swarm.NotifyToolResult.String() || event.ToolCallID != "original-report" {
					t.Fatalf("recovered receipt did not reach the actual bus: %+v", event)
				}
			default:
				t.Fatal("recovered tool result was not broadcast")
			}
		})
	}
}

func TestUnpairedPrivateReportsCannotInventToolResults(t *testing.T) {
	for _, mode := range []string{"missing-call", "wrong-tool", "malformed", "receipt-exists", "business-refused", "write-failure", "deleted-turn"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("private report refusal", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			if mode != "missing-call" {
				name := ToolReportSchedule
				if mode == "wrong-tool" {
					name = ToolScheduleWake
				}
				if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: name + `({})`}); err != nil {
					t.Fatal(err)
				}
			}
			var raw *string
			if mode == "malformed" {
				body := `{`
				raw = &body
			}
			saveRestoreAnswer(t, e, turn, "private-report", "report", "pending", raw)
			if mode == "receipt-exists" || mode == "business-refused" {
				text := `{"ok":true,"quiet":false,"report_pending":true}`
				if mode == "business-refused" {
					text = `{"ok":false,"code":"goal_schedule_conflict"}`
				}
				if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: text}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "write-failure" {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_recovered_report BEFORE INSERT ON events WHEN NEW.kind='tool_result' BEGIN SELECT RAISE(ABORT, 'receipt unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "deleted-turn" {
				e.droppedTurns[turn.ID] = struct{}{}
			}
			err := e.restorePendingScheduleReports(turn, nil)
			wantError := mode == "missing-call" || mode == "wrong-tool" || mode == "malformed" || mode == "write-failure" || mode == "deleted-turn"
			if (err != nil) != wantError {
				t.Fatalf("report recovery error=%v mode=%s", err, mode)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			results := 0
			for _, event := range events {
				if event.Kind == swarm.NotifyToolResult.String() {
					results++
				}
			}
			wantResults := 0
			if mode == "receipt-exists" || mode == "business-refused" {
				wantResults = 1
			}
			if results != wantResults {
				t.Fatalf("report recovery fabricated a result: %d", results)
			}
		})
	}
}

func TestRecoveredReportRetainsUnknownHistoryAndRestoresItsConfirmedReceipt(t *testing.T) {
	for _, placeholder := range []string{swarm.HandoffUnknownToolResult, resumeToolStopped, swarm.InterruptedToolResult} {
		for _, beforeCheckpoint := range []bool{false, true} {
			t.Run(placeholder+map[bool]string{true: "-before", false: "-after"}[beforeCheckpoint], func(t *testing.T) {
				e := newTestEngine(t)
				th, _ := e.CreateThread("confirmed report after unknown", "", "")
				turn := mustStoredTurn(t, e, th.ID, store.Turn{})
				call := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: ToolReportSchedule + `({})`}
				unknown := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: placeholder}
				checkpoint := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: `{"version":1}`}
				if err := e.store.AppendEvent(&call); err != nil {
					t.Fatal(err)
				}
				ordered := []*store.Event{&checkpoint, &unknown}
				if beforeCheckpoint {
					ordered = []*store.Event{&unknown, &checkpoint}
				}
				for _, event := range ordered {
					if err := e.store.AppendEvent(event); err != nil {
						t.Fatal(err)
					}
				}
				saveRestoreAnswer(t, e, turn, "private-report", "report", "pending", nil)
				events, _ := e.store.ListTurnEvents(turn.ID)
				snapshot := &restoreSnapshot{threadID: th.ID, events: events}
				for range 2 {
					if err := e.restorePendingScheduleReports(turn, snapshot); err != nil {
						t.Fatal(err)
					}
				}
				results := []store.Event{}
				for _, event := range snapshot.events {
					if event.Kind == swarm.NotifyToolResult.String() {
						results = append(results, event)
					}
				}
				if len(results) != 2 || results[0].Text != placeholder || results[1].ToolCallID != "original-report" || !strings.Contains(results[1].Text, `"report_pending":true`) {
					t.Fatalf("recovery erased unknown evidence or repeated the action receipt: %+v", results)
				}
			})
		}
	}
}

func saveRestoreAnswer(t *testing.T, e *Engine, turn *store.Turn, id, kind, state string, raw *string) *store.PendingScheduleAnswer {
	t.Helper()
	message := schema.AssistantMessage("The inspected state is ready.", nil)
	message.Extra = map[string]any{pendingScheduleAnswerKey: id}
	eventKind := swarm.NotifyAgentMessage.String()
	if kind == "report" {
		eventKind = KindScheduleReport
	}
	body, err := json.Marshal(map[string]any{
		"version": 1, "answer_id": id, "text": []string{message.Content}, "final": message.Content,
		"transcript": []*schema.Message{message}, "events": []store.Event{{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: eventKind, AgentID: swarm.DefaultManagerID, Text: message.Content}},
		"tool_call_id": "original-report", "tool_result": `{"ok":true,"quiet":false,"report_pending":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		body = []byte(*raw)
	}
	row := &store.PendingScheduleAnswer{ID: id, ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: kind, State: "pending", Payload: string(body), CreatedAt: time.Now().UTC()}
	if err := e.store.SavePendingScheduleAnswer(row); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		if err := e.store.DB().Model(&store.PendingScheduleAnswer{}).Where("id = ?", id).Update("state", state).Error; err != nil {
			t.Fatal(err)
		}
		row.State = state
	}
	return row
}

func TestRestorePendingWorkersKeepsFinishedCollectionAndStartsTheOriginalWorker(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("pending worker recovery", "", "")
	set := buildTestToolset(t, e, th.ID)
	entered, release := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeContextFlowReply(w, true, map[string]any{"content": "Recovered worker finished."}, "stop")
	}))
	defer endpoint.Close()
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-pending-worker"
	e.pool = provider.New(e.cfg)
	builder, err := e.pool.ModelBuilder(context.Background(), "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, nil)
	defer reg.Close()
	notes := make(chan swarm.Notification, 32)
	reg.SetHostNotify(func(note swarm.Notification) { notes <- note })
	rt := e.runtimeFor(th.ID)
	rt.setWorkerRestore([]swarm.RestoredWorker{{ID: "restored-1", Role: "inspector", Task: "Inspect the current state.", Generation: 7}}, []swarm.FinishedWorker{{ID: "finished-2", Role: "inspector", Result: "Earlier work completed.", Generation: 4, Collected: true}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.restorePendingScheduleWorkers(ctx, reg); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the original restored worker never reached the real HTTP adapter")
	}
	workers := reg.SnapshotWorkers()
	if len(workers) != 2 || workers[0].ID != "finished-2" || !workers[0].Collected || workers[0].Generation != 4 || workers[1].ID != "restored-1" || workers[1].Generation != 7 {
		t.Fatalf("pending answer recovery changed worker identities: %+v", workers)
	}
	if remaining, finished := rt.takeWorkerRestore(); len(remaining) != 0 || len(finished) != 0 {
		t.Fatal("worker restoration was not consumed exactly once")
	}
	close(release)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case note := <-notes:
			if note.Kind == swarm.NotifyFinished {
				if note.AgentID != "restored-1" || note.Text != "Recovered worker finished." || note.Err != nil {
					t.Fatalf("recovery changed its real worker completion: %+v", note)
				}
				return
			}
		case <-deadline.C:
			t.Fatal("restored worker did not retain its host completion notification")
		}
	}
}

func TestPendingWorkerRecoveryPreservesRegistryValidationErrors(t *testing.T) {
	for _, finished := range []bool{false, true} {
		e := newTestEngine(t)
		th, _ := e.CreateThread("invalid restored worker", "", "")
		rt := e.runtimeFor(th.ID)
		reg := swarm.NewRegistry()
		if finished {
			rt.setWorkerRestore(nil, []swarm.FinishedWorker{{}})
		} else {
			rt.setWorkerRestore([]swarm.RestoredWorker{{ID: "invalid-1", Role: "inspector"}}, nil)
		}
		if err := rt.restorePendingScheduleWorkers(context.Background(), reg); err == nil {
			t.Fatal("invalid worker restoration became success")
		}
		reg.Close()
	}
}

func TestReportRecoveryReusesTheSingleFilteredSnapshotAcrossCheckpointOrder(t *testing.T) {
	for _, beforeCheckpoint := range []bool{true, false} {
		t.Run(map[bool]string{true: "report-before-checkpoint", false: "report-after-checkpoint"}[beforeCheckpoint], func(t *testing.T) {
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Memory.AutoReview = false
			th, _ := e.CreateThread("filtered report recovery", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue existing work")
			call := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: ToolReportSchedule + `({})`}
			unknown := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-report", Text: swarm.InterruptedToolResult}
			appendEvent := func(event *store.Event) {
				if err := e.store.AppendEvent(event); err != nil {
					t.Fatal(err)
				}
			}
			appendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: turn.UserText})
			checkpoint := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}}
			if beforeCheckpoint {
				appendEvent(&call)
				appendEvent(&unknown)
				checkpoint.ManagerHistory = append(checkpoint.ManagerHistory,
					schema.AssistantMessage("", []schema.ToolCall{{ID: call.ToolCallID, Type: "function", Function: schema.FunctionCall{Name: ToolReportSchedule, Arguments: `{}`}}}),
					schema.ToolMessage(unknown.Text, call.ToolCallID))
			}
			body, _ := json.Marshal(checkpoint)
			appendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
			if !beforeCheckpoint {
				appendEvent(&call)
				appendEvent(&unknown)
			}
			saveRestoreAnswer(t, e, turn, "private-report", "report", "rejected", nil)
			var mu sync.Mutex
			checkpointReads, filteredReads, fullReads := 0, 0, 0
			bootstrapDone := false
			if err := e.store.DB().Callback().Create().After("gorm:create").Register("report_bootstrap_done", func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "generation_started" {
					mu.Lock()
					bootstrapDone = true
					mu.Unlock()
				}
			}); err != nil {
				t.Fatal(err)
			}
			countRead := func(tx *gorm.DB) {
				if tx.Statement.Table != "events" {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if bootstrapDone {
					return
				}
				query := tx.Statement.SQL.String()
				if strings.Contains(query, "kind =") && strings.Contains(query, "ORDER BY seq desc") {
					checkpointReads++
				}
				if strings.Contains(query, "AND (kind IN") {
					filteredReads++
				}
				if !strings.Contains(query, "kind") && (strings.Contains(query, "seq >") || strings.Contains(query, "turn_id =")) {
					fullReads++
				}
			}
			for _, err := range []error{
				e.store.DB().Callback().Query().After("gorm:query").Register("report_recovery_reads", countRead),
				e.store.DB().Callback().Row().After("gorm:row").Register("report_recovery_reads", countRead),
			} {
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := e.Restore(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("filtered report restore failed: %+v", got)
			}
			mu.Lock()
			checkpoints, filtered, full := checkpointReads, filteredReads, fullReads
			mu.Unlock()
			if checkpoints != 1 || filtered != 1 || full != 0 {
				t.Fatalf("report recovery escaped its shared snapshot: checkpoint=%d filtered=%d full=%d", checkpoints, filtered, full)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			results := []store.Event{}
			for _, event := range events {
				if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == call.ToolCallID {
					results = append(results, event)
				}
			}
			if len(results) != 2 || results[0].Text != unknown.Text || !strings.Contains(results[1].Text, `"report_pending":true`) {
				t.Fatalf("the filtered read lost the original call or its unknown receipt: %+v", results)
			}
		})
	}
}

func TestReportRecoverySnapshotRefusesUnreadableOrMalformedPrivateState(t *testing.T) {
	for _, mode := range []string{"unreadable", "malformed", "missing-call-id"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("private snapshot refusal", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			if mode == "unreadable" {
				if err := e.store.DB().Migrator().DropTable(&store.PendingScheduleAnswer{}); err != nil {
					t.Fatal(err)
				}
			} else {
				body := `{`
				if mode == "missing-call-id" {
					body = `{"version":1,"answer_id":"private-report"}`
				}
				saveRestoreAnswer(t, e, turn, "private-report", "report", "pending", &body)
			}
			if snapshot, err := e.loadRestoreSnapshot(context.Background(), th.ID); err == nil || snapshot != nil {
				t.Fatalf("invalid private state authorized a partial recovery snapshot: %+v %v", snapshot, err)
			}
		})
	}
}

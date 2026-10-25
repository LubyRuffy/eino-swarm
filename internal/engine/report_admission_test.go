package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Equal findings are separate reports when actual tool call IDs differ. The
// model must receive their committed receipts before any policy service wait.
func TestReportToolsKeepFindingsPrivateUntilTheManagerBoundary(t *testing.T) {
	const findings = "The requested inspection is complete."
	var managerCalls, policyCalls atomic.Int32
	receipts := make(chan []*schema.Message, 1)
	releaseFinal := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFinal) }) }
	t.Cleanup(release)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		system := req.Messages[0].Content
		if strings.HasPrefix(system, "## Schedule answer claims") {
			policyCalls.Add(1)
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		if strings.HasPrefix(system, "## Thread wake authorization classification") {
			writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
			return
		}
		if !strings.Contains(system, "## Waiting") {
			t.Errorf("unexpected auxiliary report fixture request: system=%q", system)
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "The inspection completed."}, "stop")
			return
		}
		if managerCalls.Add(1) == 1 {
			calls := make([]any, 0, 2)
			for i, id := range []string{"report-first", "report-second"} {
				calls = append(calls, map[string]any{"index": i, "id": id, "type": "function", "function": map[string]any{
					"name": ToolReportSchedule, "arguments": `{"findings":"` + findings + `","next_in_s":90}`,
				}})
			}
			writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": calls}, "tool_calls")
			return
		}
		receipts <- req.Messages
		select {
		case <-releaseFinal:
		case <-r.Context().Done():
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": ""}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-report-admission"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("report boundary", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 120,
		Title: "Report admission", Prompt: "Inspect the current work.", CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Exec("CREATE TABLE report_rearm_audit (schedule_id TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Exec("CREATE TRIGGER audit_report_rearm AFTER UPDATE OF every_s ON schedules BEGIN INSERT INTO report_rearm_audit VALUES (NEW.id); END").Error; err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	var messages []*schema.Message
	select {
	case messages = <-receipts:
	case <-time.After(5 * time.Second):
		t.Fatal("report tools did not return their committed receipts")
	}
	results := map[string]string{}
	for _, msg := range messages {
		if msg.Role != schema.Tool || !strings.HasPrefix(msg.ToolCallID, "report-") {
			continue
		}
		var receipt struct {
			OK            bool `json:"ok"`
			Quiet         bool `json:"quiet"`
			ReportPending bool `json:"report_pending"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &receipt); err != nil || !receipt.OK || receipt.Quiet || !receipt.ReportPending || strings.Contains(msg.Content, findings) {
			t.Fatalf("report was not deferred privately: call=%s result=%s error=%v", msg.ToolCallID, msg.Content, err)
		}
		results[msg.ToolCallID] = msg.Content
	}
	if len(results) != 2 || policyCalls.Load() != 0 {
		t.Fatalf("tools classified before the manager boundary: receipts=%v policy=%d", results, policyCalls.Load())
	}
	for id, result := range results {
		row, err := e.store.GetPendingScheduleAnswer(turn.ID + ":" + id)
		if err != nil || row.Kind != "report" || row.State != "pending" {
			t.Fatalf("missing exact-call candidate %s: %+v error=%v", id, row, err)
		}
		var payload struct {
			Text           []string      `json:"text"`
			Events         []store.Event `json:"events"`
			ToolCallID     string        `json:"tool_call_id"`
			ToolResult     string        `json:"tool_result"`
			CommittedRearm []string      `json:"committed_rearm"`
		}
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil || payload.ToolCallID != id || payload.ToolResult != result || len(payload.Events) != 1 || payload.Events[0].Kind != KindScheduleReport || len(payload.Text) != 1 || payload.Text[0] != findings || len(payload.CommittedRearm) != 1 || payload.CommittedRearm[0] != sch.ID {
			t.Fatalf("candidate lost its report/call identity: %+v error=%v", payload, err)
		}
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	for _, ev := range events {
		if ev.Kind == KindScheduleReport {
			t.Fatalf("unverified findings were published: %+v", ev)
		}
	}
	var rearms int64
	if err := e.store.DB().Table("report_rearm_audit").Count(&rearms).Error; err != nil || rearms != 2 {
		t.Fatalf("report action count=%d; each distinct tool call must commit once: %v", rearms, err)
	}
	release()
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone || managerCalls.Load() != 2 || policyCalls.Load() != 2 {
		t.Fatalf("report admission repeated manager actions: turn=%+v manager=%d policy=%d", got, managerCalls.Load(), policyCalls.Load())
	}
	if err := e.store.DB().Table("report_rearm_audit").Count(&rearms).Error; err != nil || rearms != 2 {
		t.Fatalf("report verification repeated committed actions: count=%d error=%v", rearms, err)
	}
}

func TestSynchronousReportVerificationDoesNotHoldTheGlobalGoalWakeLock(t *testing.T) {
	entered, releasePolicy := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(releasePolicy) }) }
	t.Cleanup(release)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-releasePolicy:
		case <-r.Context().Done():
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-report-lock"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("report lock", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	ctx, done := ownManagerTestFixture(context.Background(), e.runtimeFor(th.ID), turn.ID, reg)
	t.Cleanup(done)
	reported := make(chan string, 1)
	go func() {
		result, _ := e.reportScheduleContextJSON(ctx, th.ID, turn.ID, `{"findings":"The inspection completed."}`)
		reported <- result
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("report classification did not reach HTTP")
	}
	unlocked := make(chan struct{})
	go func() {
		e.goalWakeMu.Lock()
		defer e.goalWakeMu.Unlock()
		close(unlocked)
	}()
	select {
	case <-unlocked:
	case <-time.After(time.Second):
		release()
		<-reported
		t.Fatal("report classification blocked unrelated goal/wake mutations")
	}
	release()
	select {
	case result := <-reported:
		if !strings.Contains(result, `"ok":true`) {
			t.Fatalf("synchronous report failed: %s", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report did not finish after policy response")
	}
}

func TestReportAdmissionRequiresActualToolIdentityBeforeMutation(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("report identity", "", "")
	sch := mustCreateWake(t, e, th.ID)
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	buffer := newManagerAnswerBuffer(newAccumulator(e, th.ID, turn.ID, 0))
	ctx := context.WithValue(context.Background(), scheduleAnswerBufferContextKey{}, buffer)
	result, err := e.reportScheduleContextJSON(ctx, th.ID, turn.ID, `{"findings":"Inspection complete.","next_in_s":90}`)
	if err != nil || !strings.Contains(result, "report_identity_unavailable") || buffer.reportError() == nil {
		t.Fatalf("unidentified report was not refused: %s error=%v", result, err)
	}
	got, err := e.store.GetSchedule(sch.ID)
	if err != nil || got.EveryS != sch.EveryS || !got.NextRunAt.Equal(sch.NextRunAt) {
		t.Fatalf("unidentified call mutated the wait: %+v error=%v", got, err)
	}
}

// Invoke the actual Eino tool node so the call identity comes from the same
// context boundary as production, including replay of one already-run call.
func TestReportAdmissionReusesItsExactToolReceiptAndRefusesBrokenState(t *testing.T) {
	for _, mode := range []string{"replay", "invalid-payload", "rejected", "read-failure", "save-failure", "save-failure-cancel", "save-failure-cancel-policy", "save-failure-no-action", "save-failure-already-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("report receipt", "", "")
			sch := mustCreateWake(t, e, th.ID)
			if mode == "save-failure-already-cancelled" {
				if err := e.CancelSchedule(sch.ID); err != nil {
					t.Fatal(err)
				}
			}
			run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
			if err := e.store.CreateRun(run); err != nil {
				t.Fatal(err)
			}
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
			reg := swarm.NewRegistry()
			defer reg.Close()
			ctx, release := ownManagerTestFixture(context.Background(), e.runtimeFor(th.ID), turn.ID, reg)
			defer release()
			buffer := newManagerAnswerBuffer(newAccumulator(e, th.ID, turn.ID, 0))
			ctx = context.WithValue(ctx, scheduleAnswerBufferContextKey{}, buffer)
			report := ReportScheduleTool(nil).(*jsonArgsTool)
			report.runContext = func(ctx context.Context, args string) (string, error) {
				return e.reportScheduleContextJSON(ctx, th.ID, turn.ID, args)
			}
			node, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{Tools: []tool.BaseTool{report}})
			if err != nil {
				t.Fatal(err)
			}
			arguments := `{"findings":"Inspection complete.","next_in_s":90}`
			switch mode {
			case "save-failure-cancel", "save-failure-already-cancelled":
				arguments = `{"findings":"Inspection complete.","keep":false}`
			case "save-failure-cancel-policy":
				arguments = `{"findings":"Inspection complete.","keep":false,"next_in_s":90}`
			case "save-failure-no-action":
				arguments = `{"findings":"Inspection complete."}`
			}
			input := schema.AssistantMessage("", []schema.ToolCall{{ID: "original-report", Type: "function", Function: schema.FunctionCall{
				Name: ToolReportSchedule, Arguments: arguments,
			}}})
			if strings.HasPrefix(mode, "save-failure") {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_report_candidate BEFORE INSERT ON pending_schedule_answers BEGIN SELECT RAISE(ABORT, 'report candidate unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			first, err := node.Invoke(ctx, input)
			if err != nil || len(first) != 1 {
				t.Fatalf("first report invocation: %+v error=%v", first, err)
			}
			if strings.HasPrefix(mode, "save-failure") {
				if failure := buffer.reportError(); failure == nil || failure.Code != "answer_storage_failed" || !strings.Contains(first[0].Content, "answer_storage_failed") {
					t.Fatalf("candidate save failure was swallowed: result=%s failure=%+v", first[0].Content, failure)
				}
				var receipt struct {
					OK              bool   `json:"ok"`
					Code            string `json:"code"`
					ActionCommitted bool   `json:"action_committed"`
					ReportPublished *bool  `json:"report_published"`
					ScheduleID      string `json:"schedule_id"`
					Error           string `json:"error"`
				}
				wantCommitted := mode != "save-failure-no-action" && mode != "save-failure-already-cancelled"
				if err := json.Unmarshal([]byte(first[0].Content), &receipt); err != nil || receipt.OK || receipt.Code != "answer_storage_failed" || receipt.ActionCommitted != wantCommitted || receipt.ReportPublished == nil || *receipt.ReportPublished || receipt.ScheduleID != sch.ID || strings.Contains(first[0].Content, "Inspection complete.") {
					t.Fatalf("save failure concealed the committed-action boundary: receipt=%+v error=%v", receipt, err)
				}
				if !strings.Contains(receipt.Error, "Inspect the current schedule") || wantCommitted && !strings.Contains(receipt.Error, "Do not repeat") {
					t.Fatalf("failure did not direct verification before retry: %s", receipt.Error)
				}
				changed, err := e.store.GetSchedule(sch.ID)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(mode, "cancel") {
					if changed.Status != store.ScheduleCancelled {
						t.Fatalf("cancellation was lost after report storage failed: %+v", changed)
					}
				} else if mode == "save-failure" && changed.EveryS != 90 {
					t.Fatalf("committed recadence was lost: %+v", changed)
				} else if mode == "save-failure-no-action" && !changed.NextRunAt.Equal(sch.NextRunAt) {
					t.Fatalf("findings-only report changed schedule: %+v", changed)
				}
				rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
				if err != nil || len(rows) != 0 {
					t.Fatalf("storage failure invented durable candidates: %+v error=%v", rows, err)
				}
				if hasKind(t, e, th.ID, KindScheduleReport) {
					t.Fatal("report storage failure published findings")
				}
				return
			}
			before, _ := e.store.GetSchedule(sch.ID)
			candidateID := turn.ID + ":original-report"
			wantFailure := ""
			switch mode {
			case "invalid-payload":
				wantFailure = "answer_state_invalid"
				err = e.store.DB().Model(&store.PendingScheduleAnswer{}).Where("id = ?", candidateID).Update("payload", "{").Error
			case "rejected":
				wantFailure = "no_matching_schedule"
				err = e.store.RejectPendingScheduleAnswer(th.ID, turn.ID, candidateID)
			case "read-failure":
				wantFailure = "answer_storage_failed"
				err = e.store.DB().Exec("ALTER TABLE pending_schedule_answers RENAME TO unavailable_report_candidates").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			second, err := node.Invoke(ctx, input)
			if err != nil || len(second) != 1 {
				t.Fatalf("repeat report invocation: %+v error=%v", second, err)
			}
			if wantFailure == "" {
				if second[0].ToolCallID != "original-report" || second[0].Content != first[0].Content {
					t.Fatalf("duplicate report lost its original receipt: first=%+v second=%+v", first, second)
				}
			} else if failure := buffer.reportError(); failure == nil || failure.Code != wantFailure || !strings.Contains(second[0].Content, wantFailure) {
				t.Fatalf("broken candidate was accepted: result=%s failure=%+v", second[0].Content, failure)
			}
			after, _ := e.store.GetSchedule(sch.ID)
			if !after.NextRunAt.Equal(before.NextRunAt) || after.EveryS != before.EveryS {
				t.Fatalf("same tool call repeated its schedule action: before=%+v after=%+v", before, after)
			}
		})
	}
}

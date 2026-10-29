package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// report_schedule is another public prose outlet. Its source protocol must be
// checked before rearming; a valid report binds the same canonical snapshots
// in its eventual publication transaction as the manager's final answer.
func TestHistoryScheduleFindingsValidateBeforeRearmAndPublishOnlyHostRendering(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "canonical", true: "invalid-before-action"}[invalid], func(t *testing.T) {
			var peerID string
			var managers atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request scheduleCompletionRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid request", 400)
					return
				}
				if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
					return
				}
				if len(request.Messages) > 0 && strings.HasPrefix(request.Messages[0].Content, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				manager := false
				for _, tool := range request.Tools {
					manager = manager || tool.Function.Name == "spawn_agent"
				}
				if !manager {
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "Session state retained."}, "stop")
					return
				}
				switch step := managers.Add(1); step {
				case 1:
					writeHistoryTool(w, request, "history-for-report", ToolQueryHistory, `{}`)
				case 2:
					findings := historyAnswerJSON(peerID)
					if invalid {
						findings = "A fabricated historical approval."
					}
					args, _ := json.Marshal(map[string]any{"findings": findings, "next_in_s": 90})
					writeHistoryTool(w, request, "history-schedule-report", ToolReportSchedule, string(args))
				case 3, 4:
					if step == 4 && !invalid {
						t.Error("valid canonical schedule report unexpectedly needed correction")
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
				default:
					t.Errorf("unexpected report manager request %d", step)
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected request."}, "stop")
				}
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Memory.AutoReview = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "history-schedule-fixture"
			e.pool = provider.New(e.cfg)
			project, err := e.CreateProject("History scheduled report", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := e.CreateThread("Original assistant record", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			peerID = peer.ID
			if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "assistant", Content: historyRecoveryOriginal}}); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Scheduled source review", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, EveryS: 120, Title: "History report verification", Prompt: "Inspect original project records.", CreatedBy: store.ScheduleCreatedManager})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Exec("CREATE TABLE history_report_rearm_audit (schedule_id TEXT NOT NULL)").Error; err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Exec("CREATE TRIGGER history_report_rearm AFTER UPDATE OF every_s ON schedules BEGIN INSERT INTO history_report_rearm_audit VALUES (NEW.id); END").Error; err != nil {
				t.Fatal(err)
			}
			turn, err := e.RunScheduleNow(wake.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || !strings.Contains(got.Final, historyRecoveryOriginal) {
				t.Fatalf("scheduled history report=%+v", got)
			}
			var rearms int64
			if err := e.store.DB().Table("history_report_rearm_audit").Count(&rearms).Error; err != nil {
				t.Fatal(err)
			}
			wantRearms, wantCalls := int64(1), int32(3)
			if invalid {
				wantRearms, wantCalls = 0, 4
			}
			if rearms != wantRearms || managers.Load() != wantCalls {
				t.Fatalf("report validation repeated/committed scheduling actions: rearms=%d manager=%d", rearms, managers.Load())
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			reports := 0
			for _, event := range events {
				if event.Kind == KindScheduleReport {
					reports++
					var body struct {
						Findings string `json:"findings"`
					}
					if json.Unmarshal([]byte(event.Text), &body) != nil || !strings.Contains(body.Findings, historyRecoveryOriginal) || strings.Contains(body.Findings, `"version":1`) {
						t.Fatalf("raw schedule findings escaped host rendering: %s", event.Text)
					}
				}
			}
			wantReports := 1
			if invalid {
				wantReports = 0
			}
			if reports != wantReports {
				t.Fatalf("public schedule reports=%d want=%d", reports, wantReports)
			}
		})
	}
}

func TestPreviouslyRearmedHistoryFindingsAreRejectedWithoutRepeatingTheAction(t *testing.T) {
	e, turn, _, _, _ := historyAnswerFixture(t)
	historyReadEvent(e, turn, "manager")
	const originalReceipt = `{"ok":true,"report_pending":true,"quiet":false}`
	payload := pendingScheduleAnswer{AnswerID: turn.ID + ":already-rearmed", Text: []string{"Unverified historical approval."}, Events: []store.Event{{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: "manager", Kind: KindScheduleReport, Text: `{"findings":"Unverified historical approval.","quiet":false}`}}, ToolCallID: "already-rearmed", ToolResult: originalReceipt, CommittedRearm: []string{"previous-action"}}
	row, failure := e.savePendingScheduleAnswer(turn, "report", payload.AnswerID, payload)
	if failure != nil {
		t.Fatal(failure)
	}
	failure = e.runtimeFor(turn.ThreadID).admitPendingScheduleReports(t.Context(), turn)
	if failure == nil || failure.Code != "history_report_invalid" {
		t.Fatalf("legacy rearmed prose bypassed report gate: %+v", failure)
	}
	stored, err := e.store.GetPendingScheduleAnswer(row.ID)
	if err != nil || stored.State != "rejected" {
		t.Fatalf("unsafe publication intent was not durably rejected: %+v %v", stored, err)
	}
	var saved pendingScheduleAnswer
	if err := json.Unmarshal([]byte(stored.Payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ToolResult != originalReceipt || saved.ToolCallID != "already-rearmed" || len(saved.CommittedRearm) != 1 {
		t.Fatal("refusal lost its already-committed action receipt")
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	for _, event := range events {
		if event.Kind == KindScheduleReport {
			t.Fatal("legacy raw history findings were published")
		}
	}
}

func TestHistorySynchronousReportUsesCanonicalEvidenceAndQuietReportsRemainQuiet(t *testing.T) {
	e, turn, peer, _, _ := historyAnswerFixture(t)
	wake := mustCreateWake(t, e, turn.ThreadID)
	run := &store.ScheduleRun{ScheduleID: wake.ID, ThreadID: turn.ThreadID, Status: store.ScheduleRunRunning}
	if err := e.store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Model(&store.Turn{}).Where("id = ?", turn.ID).Updates(map[string]any{"schedule_continue": true, "schedule_run_id": run.ID}).Error; err != nil {
		t.Fatal(err)
	}
	historyReadEvent(e, turn, "manager")
	args, _ := json.Marshal(map[string]any{"findings": historyAnswerJSON(peer.ID)})
	body, err := e.reportScheduleContextJSON(t.Context(), turn.ThreadID, turn.ID, string(args))
	if err != nil || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("direct source report=%s err=%v", body, err)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	found := false
	for _, event := range events {
		if event.Kind == KindScheduleReport {
			found = true
			if strings.Contains(event.Text, `\"version\":1`) || !strings.Contains(event.Text, "not implemented") {
				t.Fatalf("direct source report bypassed rendering: %s", event.Text)
			}
		}
	}
	if !found {
		t.Fatal("direct report was not admitted")
	}
	if failure := e.admitScheduleAnswer(turn.ThreadID, turn.ID, nil, []store.Event{{Kind: KindScheduleReport, AgentID: "manager", Text: `{"findings":"","quiet":true}`}}); failure != nil {
		t.Fatal("empty findings incorrectly require a fabricated source report", failure)
	}
	if failure := e.admitScheduleAnswer(turn.ThreadID, turn.ID, nil, []store.Event{{Kind: KindScheduleReport, AgentID: "manager", Text: `not valid report JSON`}}); failure == nil || failure.Code != "history_report_invalid" {
		t.Fatalf("malformed history report event was admitted: %+v", failure)
	}
}

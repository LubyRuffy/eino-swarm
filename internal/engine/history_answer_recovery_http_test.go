package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const historyRecoveryOriginal = "The operation is not implemented; deletion is forbidden unless explicitly approved."

func newHistoryPolicyFixture(t *testing.T, policy func(*Engine, http.ResponseWriter, *http.Request, int32)) (*Engine, *store.Thread, *store.Thread, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var e *Engine
	var peerID string
	managers, checks := &atomic.Int32{}, &atomic.Int32{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		if len(request.Messages) > 0 && strings.HasPrefix(request.Messages[0].Content, "## Schedule answer claims") {
			n := checks.Add(1)
			if request.Stream || len(request.Tools) > 0 || request.ToolChoice != "none" {
				t.Error("history report changed schedule policy isolation")
			}
			if policy != nil {
				policy(e, w, r, n)
			} else {
				writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			}
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The session remains available."}, "stop")
			return
		}
		if managers.Add(1) == 1 {
			writeHistoryTool(w, request, "original-history-call", ToolQueryHistory, `{}`)
		} else {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "history-pending-fixture"
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Pending history report", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original full text", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.store.AppendMessages(peer.ID, "", []store.Message{{Role: "assistant", Content: historyRecoveryOriginal}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Pending publication", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, thread, peer, managers, checks
}

func TestHistorySourceChangeDuringActualPolicyCannotPublishItsStaleRendering(t *testing.T) {
	const corrected = "The later original correction still forbids deletion, and implementation remains unverified."
	var sourceThread string
	e, thread, peer, managers, checks := newHistoryPolicyFixture(t, func(e *Engine, w http.ResponseWriter, _ *http.Request, n int32) {
		if n == 1 {
			if err := e.store.DB().Model(&store.Message{}).Where("thread_id = ?", sourceThread).Update("content", corrected).Error; err != nil {
				t.Error(err)
			}
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	sourceThread = peer.ID
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !strings.Contains(got.Final, corrected) || strings.Contains(got.Final, historyRecoveryOriginal) || managers.Load() != 3 || checks.Load() != 2 {
		t.Fatalf("stale source escaped or tool work repeated: %+v manager=%d policy=%d", got, managers.Load(), checks.Load())
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	reads := 0
	for _, event := range events {
		if event.Kind == "tool_call" && toolCallName(event.Text) == ToolQueryHistory {
			reads++
		}
		if event.Kind == "agent_message" && strings.Contains(event.Text, historyRecoveryOriginal) {
			t.Fatal("stale rendered evidence was published")
		}
	}
	if reads != 1 {
		t.Fatalf("repair repeated original query: %d", reads)
	}
}

func TestPendingHistoryReportSurvivesActualHandoffWithoutAnotherManagerRequest(t *testing.T) {
	entered := make(chan struct{})
	e, thread, _, managers, checks := newHistoryPolicyFixture(t, func(_ *Engine, w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			close(entered)
			<-r.Context().Done()
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("report did not reach actual policy request")
	}
	row, payload, failure := e.restorePendingScheduleAnswer(turn)
	if failure != nil || row == nil || payload.HistoryReport == nil {
		t.Fatalf("no durable typed candidate before handoff: %+v %+v", row, failure)
	}
	originalID, rendered := row.ID, payload.Final
	if strings.Contains(replayDump(t, e, thread.ID), historyRecoveryOriginal) {
		t.Fatal("pending report reached replay before admission")
	}
	next := coldRestartReplayEngine(t, e)
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("history handoff resume=%d %v", count, err)
	}
	got := waitPendingRecoveryTurn(t, next, turn.ID)
	waitSettled(t, next, thread.ID)
	if got.Status != store.TurnDone || got.Final != rendered || managers.Load() != 2 || checks.Load() != 2 {
		t.Fatalf("history recovery repeated or rewrote model work: %+v manager=%d policy=%d", got, managers.Load(), checks.Load())
	}
	accepted, err := next.store.GetPendingScheduleAnswer(originalID)
	if err != nil || accepted.State != "accepted" {
		t.Fatalf("candidate identity not retained: %+v %v", accepted, err)
	}
	events, _ := next.store.ListTurnEvents(turn.ID)
	answers := 0
	for _, event := range events {
		if event.Kind == "agent_message" {
			answers++
			if event.Text != rendered {
				t.Fatal("restored rendering changed")
			}
		}
	}
	if answers != 1 {
		t.Fatalf("recovery answer publications=%d", answers)
	}
}

func TestActualStopOrDeletionWithholdsPendingCanonicalHistoryReport(t *testing.T) {
	for _, action := range []string{"stop", "delete"} {
		t.Run(action, func(t *testing.T) {
			entered := make(chan struct{})
			e, thread, _, managers, _ := newHistoryPolicyFixture(t, func(_ *Engine, _ http.ResponseWriter, r *http.Request, n int32) {
				if n == 1 {
					close(entered)
				}
				<-r.Context().Done()
			})
			turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("pending canonical report did not reach policy")
			}
			if action == "stop" {
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				got := waitForTurn(t, e, turn.ID)
				if got.Status != store.TurnCancelled || got.Final != "" {
					t.Fatalf("Stop published a history report: %+v", got)
				}
			} else {
				if err := e.DeleteThread(thread.ID); err != nil {
					t.Fatal(err)
				}
			}
			waitSettled(t, e, thread.ID)
			if managers.Load() != 2 {
				t.Fatal("Stop/deletion replayed manager tools")
			}
			if action == "stop" {
				messages, err := e.store.ListMessages(thread.ID)
				if err != nil {
					t.Fatal(err)
				}
				preservedCalls := 0
				for _, message := range messages {
					if message.Role == "assistant" && message.Content == "" && strings.Contains(message.ToolCalls, ToolQueryHistory) {
						preservedCalls++
					}
				}
				if preservedCalls != 1 {
					t.Fatalf("Stop lost its actual completed history tool call: %+v", messages)
				}
			}
			for _, table := range []string{"messages", "events", "pending_schedule_answers"} {
				var count int64
				query := e.store.DB().Table(table).Where("turn_id = ?", turn.ID)
				if table == "messages" {
					query = query.Where("role = ? AND content <> ?", "assistant", "")
				}
				if table == "events" {
					query = query.Where("kind = ?", "agent_message")
				}
				if table == "pending_schedule_answers" && action == "stop" {
					query = query.Where("payload <> ?", "")
				}
				if err := query.Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("withheld/deleted report survived in %s: count=%d err=%v", table, count, err)
				}
			}
		})
	}
}

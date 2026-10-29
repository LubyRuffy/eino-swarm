package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Cold parked recovery has no live manager context. Bind the actual checkpoint
// owner before restoring the worker; a later check inherits only that Registry,
// while a newly constructed Registry on the same thread stays independent.
func TestHistoryColdParkedWorkerKeepsItsOwnerAndOnlyTheReusedRegistryInheritsReportMode(t *testing.T) {
	for _, tc := range []struct {
		name             string
		finished, resume bool
	}{
		{name: "running-parked"},
		{name: "finished-uncollected-fresh-registry", finished: true},
		{name: "unfinished-successor-resumes-with-unread-result", finished: true, resume: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finishedBeforeRestart := tc.finished
			const original = "The earlier original statement explicitly excludes automatic deletion."
			const ordinary = "This new ordinary request does not reuse the previous registry."
			var managers, workers atomic.Int32
			var peerID, workerID string
			ready, releaseRead := make(chan struct{}), make(chan struct{})
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
				if replyHistorySessionMemoryFixture(t, w, request) {
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
					switch n := workers.Add(1); n {
					case 1:
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"cold-source\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Waiting before the original read.\"},\"finish_reason\":null}]}\n\n")
						w.(http.Flusher).Flush()
						close(ready)
						if !finishedBeforeRestart {
							<-r.Context().Done()
							return
						}
						select {
						case <-releaseRead:
						case <-r.Context().Done():
							return
						}
						args, _ := json.Marshal(map[string]any{"sql": "SELECT content FROM messages WHERE thread_id = ?", "params": []string{peerID}})
						writeHistoryTool(w, request, "cold-parked-source", ToolQueryHistory, string(args))
					case 2:
						if !finishedBeforeRestart {
							args, _ := json.Marshal(map[string]any{"sql": "SELECT content FROM messages WHERE thread_id = ?", "params": []string{peerID}})
							writeHistoryTool(w, request, "cold-parked-source", ToolQueryHistory, string(args))
							return
						}
						fallthrough
					case 3:
						found := false
						var toolBodies []string
						for _, message := range request.Messages {
							found = found || message.Role == "tool" && strings.Contains(message.Content, original) && strings.Contains(message.Content, `"ok":true`)
							if message.Role == "tool" {
								toolBodies = append(toolBodies, message.Content)
							}
						}
						if !found {
							t.Errorf("cold worker never received original SQL result: tool receipts=%v", toolBodies)
						}
						writeContextFlowReply(w, request.Stream, map[string]any{"content": "Original source retained: " + original}, "stop")
					default:
						t.Errorf("unexpected cold worker request %d", n)
						writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected worker request."}, "stop")
					}
					return
				}
				switch n := managers.Add(1); n {
				case 1:
					writeHistoryTool(w, request, "cold-parked-spawn", "spawn_agent", `{"role":"cold-source-reader","task":"Read original records after recovery and retain their identities."}`)
				case 2:
					for _, message := range request.Messages {
						if message.Role == "tool" && message.ToolCallID == "cold-parked-spawn" {
							var receipt struct {
								AgentID string `json:"agent_id"`
							}
							if json.Unmarshal([]byte(message.Content), &receipt) == nil {
								workerID = receipt.AgentID
							}
						}
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker remains available for the existing wake."}, "stop")
				case 3:
					args, _ := json.Marshal(map[string]any{"agent_ids": []string{workerID}, "timeout_s": 10})
					writeHistoryTool(w, request, "cold-parked-collect", "wait_agents", string(args))
				case 4:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
				default:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": ordinary}, "stop")
				}
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Memory.AutoReview = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "history-cold-parked"
			e.pool = provider.New(e.cfg)
			project, err := e.CreateProject("Cold parked history", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := e.CreateThread("Original exclusion", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			peerID = peer.ID
			if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: original}}); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Cold registry history", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, EveryS: 3600, Title: "Cold parked source owner", Prompt: "Inspect ongoing work.", CreatedBy: store.ScheduleCreatedHuman})
			if err != nil {
				t.Fatal(err)
			}
			first, err := e.StartTurn(thread.ID, "Start the background source inspection.")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start before cold recovery")
			}
			if got := waitForTurn(t, e, first.ID); got.Status != store.TurnDone {
				t.Fatalf("first manager did not park: %+v", got)
			}
			waitSettled(t, e, thread.ID)
			if finishedBeforeRestart {
				close(releaseRead)
				waitRestartState(t, func() bool {
					events, _ := e.store.ListTurnEvents(first.ID)
					for _, event := range events {
						if event.Kind == "finished" && event.AgentID == workerID {
							return true
						}
					}
					return false
				})
			}
			var second *store.Turn
			if tc.resume {
				// This is the durable crash window after turn creation, before
				// a runtime has attached the prior worker state.
				second = mustStoredTurn(t, e, thread.ID, store.Turn{UserText: "Collect and inspect the existing source result."})
				if err := e.store.AppendMessages(thread.ID, second.ID, []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: second.UserText}}); err != nil {
					t.Fatal(err)
				}
			}
			next := coldRestartReplayEngine(t, e)
			wantRestored, wantWorkerRequests := 1, int32(3)
			if finishedBeforeRestart {
				wantRestored, wantWorkerRequests = 0, 2
			}
			if count, err := next.ResumeParkedWorkers(); err != nil || count != wantRestored {
				t.Fatalf("cold parked recovery=%d %v", count, err)
			}
			waitRestartState(t, func() bool {
				events, _ := next.store.ListTurnEvents(first.ID)
				for _, event := range events {
					if event.Kind == "finished" && event.AgentID == workerID {
						if event.Err != "" {
							t.Errorf("cold parked read failed: %s", event.Err)
						}
						return true
					}
				}
				return false
			})
			if managers.Load() != 2 || workers.Load() != wantWorkerRequests || next.Status(thread.ID).Running {
				t.Fatalf("parked recovery invented a manager or failed its actual query: manager=%d worker=%d", managers.Load(), workers.Load())
			}
			ownerMarker := false
			events, _ := next.store.ListTurnEvents(first.ID)
			for _, event := range events {
				ownerMarker = ownerMarker || event.Kind == "history_read_started" && event.ToolCallID == "cold-parked-source"
			}
			if !ownerMarker {
				t.Fatal("cold worker lost its actual checkpoint owner")
			}
			if tc.resume {
				if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
					t.Fatalf("unfinished successor recovery=%d %v", count, err)
				}
			} else {
				second, err = next.StartTurn(thread.ID, "Collect and inspect the existing source result.")
				if err != nil {
					t.Fatal(err)
				}
			}
			got := waitForTurn(t, next, second.ID)
			waitSettled(t, next, thread.ID)
			if got.Status != store.TurnDone || !strings.Contains(got.Final, original) || strings.Contains(got.Final, `"version":1`) {
				t.Fatalf("successor failed to inherit canonical report mode: %+v", got)
			}
			edge := false
			events, _ = next.store.ListTurnEvents(second.ID)
			for _, event := range events {
				edge = edge || event.Kind == "history_registry_inherited" && event.SourceTurnID == first.ID
			}
			if !edge {
				t.Fatal("actual registry reuse did not bind its prior owner")
			}
			if err := next.CancelSchedule(wake.ID); err != nil {
				t.Fatal(err)
			}
			third, err := next.StartTurn(thread.ID, "Answer a new unrelated ordinary request.")
			if err != nil {
				t.Fatal(err)
			}
			plain := waitForTurn(t, next, third.ID)
			waitSettled(t, next, thread.ID)
			if plain.Status != store.TurnDone || plain.Final != ordinary {
				t.Fatalf("history mode leaked to a different registry: %+v", plain)
			}
			if inherited, err := next.store.TurnHasHistoryQuery(thread.ID, third.ID); err != nil || inherited {
				t.Fatalf("unrelated registry inherited source mode: %t %v", inherited, err)
			}
		})
	}
}

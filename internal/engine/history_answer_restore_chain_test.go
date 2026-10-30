package engine

import (
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A crash may leave one indexed checkpoint plus later generations attributed
// to different turns. Only a durable Registry relationship can join those
// owners; sharing a thread or having adjacent turn sequence numbers cannot.
func TestFreshRegistryRestoresUnreadWorkersOnlyWhenTheirRecordedOwnersHaveAProvenChain(t *testing.T) {
	for _, proven := range []bool{false, true} {
		name := "unrelated-owners-refuse-before-model"
		if proven {
			name = "existing-registry-chain-restores-both-results"
		}
		t.Run(name, func(t *testing.T) {
			var peerID string
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					writeHistoryTool(w, request, "collect-crash-generations", "wait_agents", `{"agent_ids":["earlier-reader","later-reader"],"timeout_s":1}`)
				case 2:
					if content := joinedContent(request.Messages); !strings.Contains(content, "Earlier generation unread result.") || !strings.Contains(content, "Later generation unread result.") {
						t.Error("real restored Registry did not return both original worker results")
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
				default:
					t.Errorf("unexpected restore manager request %d", step)
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected extra request."}, "stop")
				}
			})
			project, err := e.CreateProject("Crash source owner chain", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := e.CreateThread("Complete original", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			peerID = peer.ID
			const original = "The original statement prohibits deletion unless separately approved."
			if err := e.store.AppendMessages(peer.ID, "", []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: original}}); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Crashed shared Registry", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			first := mustStoredTurn(t, e, thread.ID, store.Turn{UserText: "Earlier generation."})
			plantFinishedWorker(t, e, thread.ID, first.ID, "earlier-reader", "reader", "Earlier generation unread result.")
			historyReportRequested(e, first)
			historyReadEvent(e, first, "earlier-reader")
			if err := e.store.FinishTurn(first.ID, store.TurnDone, "Earlier check ended.", ""); err != nil {
				t.Fatal(err)
			}
			second := mustStoredTurn(t, e, thread.ID, store.Turn{UserText: "Later generation."})
			plantFinishedWorker(t, e, thread.ID, second.ID, "later-reader", "reader", "Later generation unread result.")
			if proven {
				if err := e.recordHistoryRegistryInheritance(t.Context(), thread.ID, second.ID, first.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.store.FinishTurn(second.ID, store.TurnDone, "Later check ended.", ""); err != nil {
				t.Fatal(err)
			}
			third, err := e.StartTurn(thread.ID, "Collect the original results after the crash.")
			if !proven {
				if err == nil || len(fixture.snapshot()) != 0 || e.Status(thread.ID).Running {
					t.Fatalf("unproved owners reached runtime/model: turn=%+v err=%v requests=%d", third, err, len(fixture.snapshot()))
				}
				return
			}
			if err != nil {
				t.Fatalf("proven original Registry owners could not restore: %v", err)
			}
			got := waitForTurn(t, e, third.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || !strings.Contains(got.Final, original) || strings.Contains(got.Final, `"version":1`) {
				t.Fatalf("restored generations did not publish canonical original text: %+v", got)
			}
			events, err := e.store.ListTurnEvents(third.ID)
			if err != nil {
				t.Fatal(err)
			}
			parents := 0
			for _, event := range events {
				if event.Kind == "history_registry_inherited" {
					parents++
					if event.SourceTurnID != second.ID {
						t.Fatalf("restored Registry chose an unproved or older owner: %+v", event)
					}
				}
			}
			if parents != 1 {
				t.Fatalf("restored Registry parents=%d, want one proven chain", parents)
			}
		})
	}
}

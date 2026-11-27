package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/easm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/taskpanel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// A cancelled or unpublishable aggregation cannot turn the source's durable
// initial snapshot into a successful result. Exercise the actual model/tool,
// MCP transport and SQLite boundary instead of calling the producer directly.
func TestEASMCollectionStopAndFailedPublicationKeepDurableEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name                         string
		stop, failWrites, failFinish bool
	}{{"stop", true, false, false}, {"publication_failure", false, false, false}, {"interruption_write_failure", true, true, false}, {"finish_write_failure", false, false, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			stop := scenario.stop
			const serverID, heatTool = "collection-fixture", "collection_heat"
			const openCall, collectCall = "collection-open", "collection-run"
			began, returned, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var requests atomic.Int32
			var remoteCancelled atomic.Bool
			remote := mcp.NewServer(&mcp.Implementation{Name: serverID, Version: "test"}, nil)
			remote.AddTool(&mcp.Tool{Name: heatTool, InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				if requests.Add(1) != 1 {
					return nil, errors.New("unexpected repeated collection")
				}
				close(began)
				defer close(returned)
				select {
				case <-ctx.Done():
					remoteCancelled.Store(true)
					return nil, context.Cause(ctx)
				case <-release:
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"hot_trend":[{"date":"2026-01-02","value":3}]}`}}}, nil
				}
			})
			endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil))
			t.Cleanup(endpoint.Close)
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					easmToolCall(w, request, openCall, "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
				case 2:
					var panel taskpanel.Panel
					if err := json.Unmarshal([]byte(scriptedToolReceipt(request, openCall)), &panel); err != nil || panel.ID == "" {
						t.Errorf("invalid open receipt: %v", err)
					}
					input := map[string]any{"panel_id": panel.ID, "input": map[string]any{
						"subject": "Collection subject", "query": easmFixtureQuery,
						"domains": []any{map[string]any{"domain": "evidence.test", "source": "observed"}},
					}}
					raw, err := json.Marshal(input)
					if err != nil {
						t.Error(err)
					}
					easmToolCall(w, request, collectCall, "run_panel_source", string(raw))
				default:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The recorded findings remain available."}, "stop")
				}
			})
			// A failed assertion must release the fixture before engine/server
			// cleanup waits for the invocation this test owns.
			t.Cleanup(unblock)
			if err := e.cfg.Update(func(c *config.Config) error {
				c.MCP.Servers = []config.MCPServer{{ID: serverID, Transport: "streamable_http", URL: endpoint.URL, Enabled: true, TimeoutSeconds: 10}}
				c.MCP.DirectLoadPercent = 0
				c.TaskPanels.EASM.ServerID = serverID
				c.TaskPanels.EASM.Tools = map[string]string{"stats": "", "heat": heatTool, "product_rules": ""}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var refused atomic.Int32
			var rejectWrites atomic.Bool
			if !stop || scenario.failWrites {
				// This private store keeps its hook through Close. The explicit
				// title and disabled automatic titles leave no unrelated title actor.
				err := e.store.DB().Callback().Update().Before("gorm:update").Register("refuse_easm_collection_write", func(tx *gorm.DB) {
					row, ok := tx.Statement.Dest.(*store.TaskPanelRow)
					if !ok {
						return
					}
					if rejectWrites.Load() {
						refused.Add(1)
						_ = tx.AddError(errors.New("injected interruption write failure"))
						return
					}
					if scenario.failFinish {
						if row.SourceStatus == "finished" {
							refused.Add(1)
							_ = tx.AddError(errors.New("injected source finish write failure"))
						}
						return
					}
					var candidate easm.State
					if json.Unmarshal(row.State, &candidate) == nil && len(candidate.Heat) != 0 {
						refused.Add(1)
						_ = tx.AddError(errors.New("injected collection publication failure"))
					}
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			thread, err := e.CreateThread("Collection lifecycle fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Collect the supplied query and retain the reported finding.")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-began:
			case <-time.After(10 * time.Second):
				t.Fatal("the aggregation never reached the real MCP tool")
			}
			panels, err := e.store.TaskPanels(t.Context(), thread.ID)
			if err != nil || len(panels) != 1 || panels[0].SourceStatus != "running" || panels[0].Source.ToolCallID != collectCall {
				t.Fatalf("initial durable source=%+v error=%v", panels, err)
			}
			initial := panels[0]
			initialChanges, err := e.store.TaskPanelChanges(t.Context(), thread.ID, 0)
			if err != nil || len(initialChanges) == 0 {
				t.Fatalf("initial delivery journal=%+v error=%v", initialChanges, err)
			}
			initialCursor := initialChanges[len(initialChanges)-1].Cursor
			if scenario.failWrites {
				rejectWrites.Store(true)
			}
			if stop {
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				unblock()
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			_, sourceActive := e.panelRuns.Load(initial.ID)
			t.Logf("manager terminal: source_active=%t remote_cancelled=%t", sourceActive, remoteCancelled.Load())
			// Stop need not cancel work already executing at a remote service.
			// Deliver the late fixture reply explicitly, then join this source;
			// its cancelled owner must still refuse to commit the returned data.
			unblock()
			select {
			case <-returned:
			case <-time.After(10 * time.Second):
				t.Fatal("the fixture MCP handler did not exit after release")
			}
			// A terminal turn does not join an ADK tool already unwinding.
			// Observe this exact source's exit before inspecting its final receipt.
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, running := e.panelRuns.Load(initial.ID); !running {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the owned source did not exit after the fixture response")
				}
				time.Sleep(10 * time.Millisecond)
			}
			panels, err = e.store.TaskPanels(t.Context(), thread.ID)
			if err != nil || len(panels) != 1 {
				t.Fatalf("settled panels=%+v error=%v", panels, err)
			}
			panel := panels[0]
			var state easm.State
			if err := json.Unmarshal(panel.State, &state); err != nil {
				t.Fatal(err)
			}
			wantSourceStatus := "interrupted"
			if scenario.failWrites {
				wantSourceStatus = initial.SourceStatus
			}
			wantHeat := 0
			if scenario.failFinish {
				wantHeat = 1
			}
			if panel.Source != initial.Source || panel.SourceStatus != wantSourceStatus || len(state.Heat) != wantHeat ||
				!slices.ContainsFunc(state.Nodes, func(node easm.Node) bool { return node.ID == "domain:evidence.test" }) || requests.Load() != 1 {
				t.Fatalf("source=%+v status=%s requests=%d state=%s", panel.Source, panel.SourceStatus, requests.Load(), panel.State)
			}
			if stop {
				if finished.Status != store.TurnCancelled {
					t.Fatalf("stopped turn=%+v", finished)
				}
				if scenario.failWrites {
					beforeDigest, beforeErr := taskpanel.Digest(initial.State)
					afterDigest, afterErr := taskpanel.Digest(panel.State)
					changes, journalErr := e.store.TaskPanelChanges(t.Context(), thread.ID, initialCursor)
					if beforeErr != nil || afterErr != nil || beforeDigest != afterDigest || refused.Load() == 0 || panel.Revision != initial.Revision || journalErr != nil || len(changes) != 0 {
						t.Fatalf("failed interruption fabricated a durable update: revision=%d/%d refused=%d changes=%+v errors=%v/%v/%v",
							initial.Revision, panel.Revision, refused.Load(), changes, beforeErr, afterErr, journalErr)
					}
					t.Logf("interruption writes rejected=%d; durable source remains %s", refused.Load(), panel.SourceStatus)
				}
				return
			}
			calls := fixture.snapshot()
			var result struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			receipt := scriptedToolReceipt(calls[len(calls)-1], collectCall)
			if finished.Status != store.TurnDone || json.Unmarshal([]byte(receipt), &result) != nil || result.OK || result.Error == "" || refused.Load() != 1 {
				t.Fatalf("publication failure: turn=%+v refused=%d receipt=%s", finished, refused.Load(), receipt)
			}
			if scenario.failFinish {
				changes, journalErr := e.store.TaskPanelChanges(t.Context(), thread.ID, initialCursor)
				if state.Collection.Status != "done" || !slices.ContainsFunc(state.Collection.Steps, func(step easm.StepStatus) bool {
					return step.ID == easm.StepHeat && step.Status == "done"
				}) || journalErr != nil || len(changes) != 3 || panel.Revision != initial.Revision+3 || slices.ContainsFunc(changes, func(change taskpanel.Change) bool {
					return change.SourceStatus == "finished"
				}) {
					t.Fatalf("failed finish lost committed heat or fabricated success: state=%s revisions=%d/%d changes=%+v error=%v", panel.State, initial.Revision, panel.Revision, changes, journalErr)
				}
				return
			}
			beforeDigest, beforeErr := taskpanel.Digest(initial.State)
			afterDigest, afterErr := taskpanel.Digest(panel.State)
			changes, journalErr := e.store.TaskPanelChanges(t.Context(), thread.ID, initialCursor)
			if beforeErr != nil || afterErr != nil || beforeDigest != afterDigest || panel.Revision != initial.Revision+1 ||
				journalErr != nil || len(changes) != 1 || changes[0].Revision != panel.Revision {
				t.Fatalf("failed publication changed data or journal: before=%s after=%s revisions=%d/%d changes=%+v errors=%v/%v/%v",
					initial.State, panel.State, initial.Revision, panel.Revision, changes, beforeErr, afterErr, journalErr)
			}
		})
	}
}

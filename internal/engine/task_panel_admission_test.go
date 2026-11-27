package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/taskpanel"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

func panelAdmissionCall(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "tool_calls")
}

// Invalid requests cross the actual HTTP model and Registry executor. A valid
// prior binding ensures errors cannot silently replace its state or version.
func TestTaskPanelInvalidCallsReturnRecoverableFeedbackAtModelHTTP(t *testing.T) {
	for _, scenario := range []string{"read-args", "open-args", "unknown-plugin", "missing-panel", "publish-args", "invalid-state", "stale-version", "update-budget", "run-args", "missing-source", "unknown-source", "invalid-source-input", "source-missing-panel", "source-busy", "source-run-failure", "read-index-budget", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			var e *Engine
			e, _ = newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					panelAdmissionCall(w, request, "admission-open", "open_task_panel", `{"plugin_id":"fofa-world-map"}`)
					return
				}
				if step == 2 {
					var panel taskpanel.Panel
					if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &panel); err != nil || panel.ID == "" {
						t.Errorf("open=%s %v", request.Messages[len(request.Messages)-1].Content, err)
						writeContextFlowReply(w, request.Stream, map[string]any{"content": "Fixture failed."}, "stop")
						return
					}
					toolName, args := "read_panel_state", `{"unexpected":true}`
					switch scenario {
					case "open-args":
						toolName, args = "open_task_panel", `{"plugin_id":3}`
					case "unknown-plugin":
						toolName, args = "open_task_panel", `{"plugin_id":"absent-plugin"}`
					case "missing-panel":
						args = `{"panel_id":"absent-panel"}`
					case "publish-args":
						toolName, args = "publish_panel_update", `{"protocol_version":"one"}`
					case "invalid-state":
						toolName, args = "publish_panel_update", fmt.Sprintf(`{"protocol_version":1,"panel_id":%q,"operation_id":"invalid","base_revision":%d,"kind":"snapshot","state":{}}`, panel.ID, panel.Revision)
					case "stale-version":
						toolName, args = "publish_panel_update", fmt.Sprintf(`{"protocol_version":1,"panel_id":%q,"operation_id":"stale","base_revision":0,"kind":"patch","operations":[]}`, panel.ID)
					case "update-budget":
						if err := e.cfg.Update(func(c *config.Config) error { c.TaskPanels.MaxUpdateBytes = 1024; return nil }); err != nil {
							t.Error(err)
						}
						toolName, args = "publish_panel_update", fmt.Sprintf(`{"protocol_version":1,"panel_id":%q,"operation_id":"budget","base_revision":%d,"kind":"patch","operations":[{"op":"add","path":"/extra","value":%q}]}`, panel.ID, panel.Revision, strings.Repeat("x", 2048))
					case "run-args":
						toolName, args = "run_panel_source", `{"panel_id":3}`
					case "missing-source", "unknown-source":
						definition := panel.Definition
						definition.Source = nil
						if scenario == "unknown-source" {
							definition.Source = &taskpanel.SourceDefinition{ID: "unregistered", InputSchema: json.RawMessage(`{"type":"object"}`)}
						}
						raw, _ := json.Marshal(definition)
						if err := e.store.DB().Model(&store.TaskPanelRow{}).Where("id = ?", panel.ID).UpdateColumn("definition_json", raw).Error; err != nil {
							t.Error(err)
						}
						toolName, args = "run_panel_source", fmt.Sprintf(`{"panel_id":%q,"input":{"query":"fixture"}}`, panel.ID)
					case "source-missing-panel":
						toolName, args = "run_panel_source", `{"panel_id":"absent-panel","input":{"query":"fixture"}}`
					case "source-busy":
						e.panelRuns.Store(panel.ID, context.CancelFunc(func() {}))
						t.Cleanup(func() { e.panelRuns.Delete(panel.ID) })
						toolName, args = "run_panel_source", fmt.Sprintf(`{"panel_id":%q,"input":{"query":"fixture"}}`, panel.ID)
					case "source-run-failure":
						if err := e.cfg.Update(func(c *config.Config) error {
							c.TaskPanels.FOFAClient = filepath.Join(t.TempDir(), "missing-client")
							return nil
						}); err != nil {
							t.Error(err)
						}
						toolName, args = "run_panel_source", fmt.Sprintf(`{"panel_id":%q,"input":{"query":"fixture"}}`, panel.ID)
					case "invalid-source-input":
						toolName, args = "run_panel_source", fmt.Sprintf(`{"panel_id":%q,"input":{}}`, panel.ID)
					case "read-index-budget":
						if err := e.cfg.Update(func(c *config.Config) error { c.Tools.ResultMaxChars = 128; return nil }); err != nil {
							t.Error(err)
						}
						args = `{}`
					case "disabled":
						if err := e.cfg.Update(func(c *config.Config) error { off := false; c.TaskPanels.Enabled = &off; return nil }); err != nil {
							t.Error(err)
						}
						args = fmt.Sprintf(`{"panel_id":%q}`, panel.ID)
					}
					panelAdmissionCall(w, request, "admission-invalid", toolName, args)
					return
				}
				body := request.Messages[len(request.Messages)-1].Content
				if !strings.Contains(body, `"error"`) && !strings.Contains(body, `"ok":false`) {
					t.Errorf("invalid call lost recoverable feedback: %s", body)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Invalid request preserved the recorded state."}, "stop")
			})
			thread, err := e.CreateThread("Panel admission fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect supplied observations and retain their identity.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone {
				t.Fatalf("turn=%+v", got)
			}
			panels, err := e.store.TaskPanels(t.Context(), thread.ID)
			if err != nil || len(panels) != 1 || (panels[0].Revision != 1 && scenario != "source-run-failure") {
				t.Fatalf("invalid call changed state: %v %v", panels, err)
			}
		})
	}
}

// The delegated worker receives panel tools through real registry construction;
// inspect its model receipt and persisted caller generation, not spawn prose.
func TestTaskPanelDelegatedWorkerPublishesAtActualHTTPBoundary(t *testing.T) {
	e := newTestEngine(t)
	var managers, workers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if len(request.Tools) == 0 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The auxiliary record is retained."}, "stop")
			return
		}
		manager := requestHasTool(request, "spawn_agent")
		if manager {
			switch managers.Add(1) {
			case 1:
				panelAdmissionCall(w, request, "panel-worker-spawn", "spawn_agent", `{"role":"observer","task":"Open the matching visualization and publish the supplied structured observation."}`)
			case 2:
				var spawned toolScopeWorkerID
				if err := json.Unmarshal([]byte(scriptedToolReceipt(request, "panel-worker-spawn")), &spawned); err != nil || spawned.AgentID == "" {
					t.Errorf("spawn=%+v %v", spawned, err)
				}
				args, _ := json.Marshal(map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
				panelAdmissionCall(w, request, "panel-worker-collect", "wait_agents", string(args))
			default:
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker observation was collected."}, "stop")
			}
			return
		}
		for _, name := range []string{"open_task_panel", "read_panel_state", "publish_panel_update", "run_panel_source"} {
			if !requestHasTool(request, name) {
				t.Errorf("worker missing %s", name)
			}
		}
		switch workers.Add(1) {
		case 1:
			panelAdmissionCall(w, request, "worker-panel-open", "open_task_panel", `{"plugin_id":"fofa-world-map"}`)
		case 2:
			var panel taskpanel.Panel
			if err := json.Unmarshal([]byte(scriptedToolReceipt(request, "worker-panel-open")), &panel); err != nil || panel.ID == "" {
				t.Errorf("worker open=%+v %v", panel, err)
			}
			args := fmt.Sprintf(`{"protocol_version":1,"panel_id":%q,"operation_id":"worker-observed","base_revision":%d,"kind":"patch","operations":[{"op":"replace","path":"/collection/phase","value":"finished"}]}`, panel.ID, panel.Revision)
			panelAdmissionCall(w, request, "worker-panel-publish", "publish_panel_update", args)
		case 3:
			if !strings.Contains(scriptedToolReceipt(request, "worker-panel-publish"), `"revision":2`) {
				t.Errorf("worker publication did not reach next model: %s", scriptedToolReceipt(request, "worker-panel-publish"))
			}
			panelAdmissionCall(w, request, "worker-panel-index", "read_panel_state", `{}`)
		default:
			var index struct {
				Plugins []taskpanel.Definition `json:"plugins"`
				Panels  []map[string]any       `json:"panels"`
			}
			if err := json.Unmarshal([]byte(scriptedToolReceipt(request, "worker-panel-index")), &index); err != nil || len(index.Plugins) == 0 || len(index.Panels) != 1 {
				t.Errorf("worker panel index=%+v %v", index, err)
			}
			if len(index.Panels) > 0 {
				if _, bulk := index.Panels[0]["state"]; bulk {
					t.Error("index leaked bulk data")
				}
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The observed data was published."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Memory.AutoDream = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "panel-worker-fixture"
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("Delegated panel fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate inspection of the supplied visualization data.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || workers.Load() != 4 {
		t.Fatalf("turn=%+v worker requests=%d", got, workers.Load())
	}
	panels, err := e.store.TaskPanels(t.Context(), thread.ID)
	if err != nil || len(panels) != 1 {
		t.Fatalf("panels=%v %v", panels, err)
	}
	source := panels[0].Source
	if source.AgentID == "manager" || source.Generation != 1 || source.ToolCallID != "worker-panel-publish" || source.TurnID != turn.ID {
		t.Fatalf("worker provenance=%+v", source)
	}
}

func TestTaskPanelSourceArgumentAndFailureCategories(t *testing.T) {
	settings := config.TaskPanelConfig{}.WithDefaults()
	for _, input := range []string{`{"extra":true}`, `{"query":"","max_assets":1}`, `{"query":"fixture","max_assets":-1}`, fmt.Sprintf(`{"query":"fixture","max_assets":%d}`, settings.MaxAssets+1)} {
		if _, err := runFOFAWorldSource(t.Context(), []byte(input), nil, settings, &tools.Set{WorkspaceDir: t.TempDir()}, func(context.Context, json.RawMessage) error { return nil }); err == nil {
			t.Fatal("source input accepted", input)
		}
	}
	if got := panelSourceError(context.Canceled); got != "cancelled" {
		t.Fatal(got)
	}
	if got := panelSourceError(context.DeadlineExceeded); got != "deadline_exceeded" {
		t.Fatal(got)
	}
	if got := panelSourceError(errors.New("private upstream details")); strings.Contains(got, "private") {
		t.Fatal(got)
	}
	e := newTestEngine(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.panelSource(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	e.wireTaskPanelRegistry(nil, nil, nil)
}

// A real prior tool receipt supplies the source; these publisher-only guards
// prove invalid/revoked updates leave that accepted version unchanged.
func TestTaskPanelPublisherRejectsRevokedAndMalformedUpdates(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			panelAdmissionCall(w, request, "publisher-open", "open_task_panel", `{"plugin_id":"fofa-world-map"}`)
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded observation retained."}, "stop")
	})
	thread, err := e.CreateThread("Publisher guards", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Retain the supplied visualization.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	panels, err := e.store.TaskPanels(t.Context(), thread.ID)
	if err != nil || len(panels) != 1 {
		t.Fatal(panels, err)
	}
	panel := panels[0]
	update := taskpanel.Update{ProtocolVersion: 1, PanelID: panel.ID, OperationID: "invalid-json", BaseRevision: panel.Revision, Kind: "snapshot", State: []byte(`{`)}
	if _, err = e.publishTaskPanel(t.Context(), panel.Source, update); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	e.recordMu.Lock()
	e.droppedTurns[turn.ID] = struct{}{}
	e.recordMu.Unlock()
	if _, err = e.publishTaskPanel(t.Context(), panel.Source, update); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("revoked turn accepted", err)
	}
	e.recordMu.Lock()
	delete(e.droppedTurns, turn.ID)
	e.recordMu.Unlock()
	if err = e.cfg.Update(func(c *config.Config) error { off := false; c.TaskPanels.Enabled = &off; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = e.publishTaskPanel(t.Context(), panel.Source, update); err == nil {
		t.Fatal("disabled update accepted")
	}
}

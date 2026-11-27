package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/easm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/taskpanel"
)

// The model emits a schema-valid direct patch containing a disconnected
// cycle. The publisher must refuse it before committing a state or delivery.
func TestEASMDirectPublishRefusesNewCycleAtomically(t *testing.T) {
	const published = `{"schema_version":1,"title":"Subject","collection":{"status":"idle","steps":[]},"nodes":[{"id":"root","kind":"root","label":"Subject"},{"id":"domain:observed.test","kind":"domain","label":"observed.test"}],"edges":[{"source":"root","target":"domain:observed.test","confirmed":true}],"heat":[],"components":[]}`
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			easmToolCall(w, request, "open-portrait", "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
			return
		}
		if step > 3 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The original observation is retained."}, "stop")
			return
		}
		var panel taskpanel.Panel
		result := request.Messages[len(request.Messages)-1].Content
		if err := json.Unmarshal([]byte(result), &panel); err != nil || panel.ID == "" {
			t.Errorf("invalid panel receipt %q: %v", result, err)
		}
		update := taskpanel.Update{ProtocolVersion: 1, PanelID: panel.ID, BaseRevision: panel.Revision}
		if step == 2 {
			update.OperationID, update.Kind, update.State = "valid-observation", "snapshot", json.RawMessage(published)
		} else {
			update.OperationID, update.Kind = "new-cycle", "patch"
			update.Operations = json.RawMessage(`[
				{"op":"add","path":"/nodes/-","value":{"id":"a","kind":"company","label":"A"}},
				{"op":"add","path":"/nodes/-","value":{"id":"b","kind":"company","label":"B"}},
				{"op":"add","path":"/edges/-","value":{"source":"a","target":"b"}},
				{"op":"add","path":"/edges/-","value":{"source":"b","target":"a"}}
			]`)
		}
		raw, err := json.Marshal(update)
		if err != nil {
			t.Error(err)
		}
		easmToolCall(w, request, fmt.Sprintf("direct-portrait-%d", step), "publish_panel_update", string(raw))
	})
	thread, err := e.CreateThread("Portrait direct publish", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Show the reported relationships.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if len(fixture.snapshot()) < 3 {
		t.Fatal("the direct update was not attempted")
	}
	panels, err := e.store.TaskPanels(t.Context(), thread.ID)
	if err != nil || len(panels) != 1 || panels[0].Revision != 2 {
		t.Fatalf("panels=%+v err=%v", panels, err)
	}
	var state easm.State
	if err := json.Unmarshal(panels[0].State, &state); err != nil || len(state.Nodes) != 2 || len(state.Edges) != 1 || state.Edges[0].Target != "domain:observed.test" {
		t.Fatalf("state=%s err=%v", panels[0].State, err)
	}
	changes, err := e.store.TaskPanelChanges(t.Context(), thread.ID, 0)
	if err != nil || len(changes) != 1 || changes[0].Revision != 2 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
}

// Read the panel back through the model's tools after one attempted publication.
// A rejected edit must leave saved relationships, mutations and delivery intact.
func TestEASMDirectPublishPreservesSavedFirstSources(t *testing.T) {
	const original = `{"schema_version":1,"title":"Subject","collection":{"status":"idle","steps":[]},"nodes":[{"id":"root","kind":"root","label":"Subject"},{"id":"domain:observed.test","kind":"domain","label":"observed.test"},{"id":"domain:unlinked.test","kind":"domain","label":"unlinked.test"}],"edges":[{"source":"root","target":"domain:observed.test","confirmed":false}],"heat":[],"components":[]}`
	for _, tc := range []struct {
		name             string
		update           taskpanel.Update
		refusal          string
		initialConfirmed bool
	}{
		{"remove_edge", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[{"op":"remove","path":"/edges/0"}]`)}, "retain the first source", false},
		{"remove_clue_and_edge", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[{"op":"remove","path":"/edges/0"},{"op":"remove","path":"/nodes/1"}]`)}, "retain the first source", false},
		{"clear_snapshot", taskpanel.Update{Kind: "snapshot", State: json.RawMessage(`{"schema_version":1,"title":"Subject","collection":{"status":"idle","steps":[]},"nodes":[],"edges":[],"heat":[],"components":[]}`)}, "saved sources requires a subject root", false},
		{"enrich_confirm_and_link", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[
			{"op":"add","path":"/nodes/1/detail","value":{"description":"Registry observation"}},
			{"op":"replace","path":"/edges/0/confirmed","value":true},
			{"op":"add","path":"/edges/-","value":{"source":"root","target":"domain:unlinked.test","confirmed":true}}
		]`)}, "", false},
		{"switch_subject", taskpanel.Update{Kind: "snapshot", State: json.RawMessage(`{"schema_version":1,"title":"Other Subject","collection":{"status":"idle","steps":[]},"nodes":[{"id":"root","kind":"root","label":"Other Subject"},{"id":"domain:other.test","kind":"domain","label":"other.test"}],"edges":[{"source":"root","target":"domain:other.test","confirmed":true}],"heat":[],"components":[]}`)}, "", false},
		{"downgrade_patch", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[{"op":"replace","path":"/edges/0/confirmed","value":false}]`)}, "retain confirmation", true},
		{"omit_confirmation_patch", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[{"op":"remove","path":"/edges/0/confirmed"}]`)}, "retain confirmation", true},
		{"replace_confirmed_edge", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[{"op":"replace","path":"/edges/0","value":{"source":"root","target":"domain:observed.test"}}]`)}, "retain confirmation", true},
		{"downgrade_snapshot", taskpanel.Update{Kind: "snapshot", State: json.RawMessage(original)}, "retain confirmation", true},
		{"omit_confirmation_snapshot", taskpanel.Update{Kind: "snapshot", State: json.RawMessage(strings.Replace(original, `,"confirmed":false`, "", 1))}, "retain confirmation", true},
		{"enrichment_cannot_hide_downgrade", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[
			{"op":"add","path":"/nodes/1/detail","value":{"description":"Registry observation"}},
			{"op":"replace","path":"/edges/0/confirmed","value":false},
			{"op":"add","path":"/edges/-","value":{"source":"root","target":"domain:unlinked.test","confirmed":true}}
		]`)}, "retain confirmation", true},
		{"retain_confirmation_with_enrichment", taskpanel.Update{Kind: "patch", Operations: json.RawMessage(`[
			{"op":"add","path":"/nodes/1/detail","value":{"description":"Registry observation"}},
			{"op":"add","path":"/edges/-","value":{"source":"root","target":"domain:unlinked.test","confirmed":true}}
		]`)}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := original
			if tc.initialConfirmed {
				initial = strings.Replace(initial, `"confirmed":false`, `"confirmed":true`, 1)
			}
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				switch step {
				case 1:
					easmToolCall(w, request, "open-portrait", "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
				case 2, 3, 4:
					previousCall := "publish-original"
					if step == 2 {
						previousCall = "open-portrait"
					}
					var panel taskpanel.Panel
					result := scriptedToolReceipt(request, previousCall)
					if err := json.Unmarshal([]byte(result), &panel); err != nil || panel.ID == "" {
						t.Errorf("invalid panel receipt %q: %v", result, err)
					}
					if step == 4 {
						easmToolCall(w, request, "read-portrait", "read_panel_state", fmt.Sprintf(`{"panel_id":%q}`, panel.ID))
						return
					}
					callID, update := "publish-candidate", tc.update
					if step == 2 {
						callID, update = "publish-original", taskpanel.Update{Kind: "snapshot", State: json.RawMessage(initial)}
					}
					update.ProtocolVersion, update.PanelID = taskpanel.ProtocolVersion, panel.ID
					update.OperationID, update.BaseRevision = callID, panel.Revision
					raw, err := json.Marshal(update)
					if err != nil {
						t.Error(err)
					}
					easmToolCall(w, request, callID, "publish_panel_update", string(raw))
				default:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The panel was read after publication."}, "stop")
				}
			})
			thread, err := e.CreateThread("Portrait first source fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Update the recorded portrait observations.")
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			requests := fixture.snapshot()
			if finished.Status != store.TurnDone || len(requests) != 5 {
				t.Fatalf("turn=%+v, model requests=%d", finished, len(requests))
			}
			var panel taskpanel.Panel
			read := scriptedToolReceipt(requests[4], "read-portrait")
			if err := json.Unmarshal([]byte(read), &panel); err != nil || panel.ID == "" {
				t.Fatalf("invalid readback %q: %v", read, err)
			}
			attempt := tc.update
			attempt.ProtocolVersion, attempt.PanelID = taskpanel.ProtocolVersion, panel.ID
			attempt.OperationID, attempt.BaseRevision = "publish-candidate", 2
			wantState, err := taskpanel.Apply(panel.Definition, json.RawMessage(initial), attempt)
			if err != nil {
				t.Fatalf("candidate must pass the panel schema: %v", err)
			}
			wantRevision, wantSource := int64(3), "publish-candidate"
			var receipt struct {
				Revision int64  `json:"revision"`
				Error    string `json:"error"`
			}
			result := scriptedToolReceipt(requests[3], "publish-candidate")
			if err := json.Unmarshal([]byte(result), &receipt); err != nil {
				t.Fatalf("invalid publication receipt %q: %v", result, err)
			}
			if tc.refusal != "" {
				if !strings.Contains(receipt.Error, tc.refusal) {
					t.Errorf("first publication should be refused: %s", result)
				}
				wantState, wantRevision, wantSource = json.RawMessage(initial), 2, "publish-original"
			} else if receipt.Error != "" || receipt.Revision != wantRevision {
				t.Errorf("legitimate publication should succeed: %s", result)
			}
			gotDigest, err := taskpanel.Digest(panel.State)
			wantDigest, wantErr := taskpanel.Digest(wantState)
			if err != nil || wantErr != nil || gotDigest != wantDigest || panel.Revision != wantRevision || panel.Source.ToolCallID != wantSource {
				t.Errorf("readback revision=%d, source=%q, state=%s, errors=%v/%v; want revision=%d, source=%q, state=%s", panel.Revision, panel.Source.ToolCallID, panel.State, err, wantErr, wantRevision, wantSource, wantState)
			}
			var mutations []store.TaskPanelMutation
			if err := e.store.DB().Where("panel_id = ?", panel.ID).Order("revision").Find(&mutations).Error; err != nil {
				t.Fatal(err)
			}
			if len(mutations) != int(wantRevision) || mutations[len(mutations)-1].OperationID != wantSource {
				t.Errorf("durable mutations changed after publication: %+v", mutations)
			}
			changes, err := e.store.TaskPanelChanges(t.Context(), thread.ID, 0)
			if err != nil || len(changes) != int(wantRevision)-1 || changes[len(changes)-1].Revision != wantRevision {
				t.Fatalf("changes=%+v err=%v", changes, err)
			}
		})
	}
}

package swarm_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type scopedHTTPTestTool string

func (s scopedHTTPTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: string(s), Desc: "Test work surface",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}

func (scopedHTTPTestTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "completed", nil
}

// Direct Restore must not reopen an existing live or archived worker with a
// wider surface, even when the new scope would be valid for a different role.
func TestWorkerToolScopeSameIDRestoreCannotWidenAtHTTPBoundary(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "finished-handle", true: "archived"}[archive], func(t *testing.T) {
			pool, fixture := newBindingHTTPPool(t)
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			var err error
			reg.ModelBuilder, err = pool.ModelBuilder(t.Context(), "fixture", "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			bindHTTPWorkerModels(reg, pool, nil)
			reg.SelectableToolNames = []string{"file_read", "file_write"}
			reg.SubAgentTools = []tool.BaseTool{scopedHTTPTestTool("file_read"), scopedHTTPTestTool("file_write")}
			body := callBindingTool(t, reg, "spawn_agent", `{"role":"scoped","task":"inspect","model_ref":"one","tool_names":["file_read"]}`)
			var spawn struct {
				ID string `json:"agent_id"`
			}
			if err := json.Unmarshal([]byte(body), &spawn); err != nil || spawn.ID == "" {
				t.Fatalf("initial scoped worker failed: %s, %v", body, err)
			}
			waitBindingWorkers(t, reg, spawn.ID)
			if archive {
				if running, pruned := reg.Stats(); running != 0 || pruned != 1 {
					t.Fatalf("archive did not prune the finished handle: running=%d pruned=%d", running, pruned)
				}
			}
			for _, names := range []*[]string{nil, &[]string{"file_read", "file_write"}, &[]string{}} {
				if archive {
					worker := swarm.FinishedWorker{ID: spawn.ID, Role: "scoped", Generation: 1, WorkerToolNames: names}
					if err := reg.PlantFinished(worker); err == nil || !strings.Contains(err.Error(), "worker tool selection cannot change") {
						t.Fatalf("existing worker scope changed on PlantFinished: %v", err)
					}
				}
				worker := swarm.RestoredWorker{ID: spawn.ID, Role: "scoped", Task: "continue", Generation: 2,
					WorkerToolNames: names}
				if _, err := reg.Restore(t.Context(), worker, nil); err == nil || !strings.Contains(err.Error(), "worker tool selection cannot change") {
					t.Fatalf("existing worker scope changed on Restore: %v", err)
				}
			}
			for _, names := range []*[]string{nil, &[]string{"file_write"}} {
				worker := swarm.RestoredWorker{ID: "child-2", Role: "child", ParentID: spawn.ID,
					Task: "inspect", Generation: 1, WorkerToolNames: names}
				if _, err := reg.Restore(t.Context(), worker, nil); err == nil || !strings.Contains(err.Error(), "would widen") {
					t.Fatalf("child restored outside its known parent's scope: %v", err)
				}
			}
			snapshot := reg.SnapshotWorkers()
			if len(snapshot) != 1 || snapshot[0].ID != spawn.ID || snapshot[0].Generation != 1 ||
				snapshot[0].WorkerToolNames == nil || !slices.Equal(*snapshot[0].WorkerToolNames, []string{"file_read"}) ||
				snapshot[0].ModelBinding == nil || snapshot[0].ModelBinding.Model != "worker-one" {
				t.Fatalf("refused Restore changed the original checkpoint: %+v", snapshot)
			}
			saved := snapshot[0]
			child := swarm.FinishedWorker{ID: "child-2", ParentID: saved.ID, Role: "child",
				Generation: 1, WorkerToolNames: &[]string{"file_read"}, Result: "read completed"}
			if err := reg.PlantFinished(child); err != nil {
				t.Fatalf("compatible child could not be restored with its known parent: %v", err)
			}
			checkpoints := reg.SnapshotWorkers()
			childIndex := slices.IndexFunc(checkpoints, func(worker swarm.WorkerCheckpoint) bool { return worker.ID == child.ID })
			if childIndex < 0 || checkpoints[childIndex].ParentID != saved.ID || checkpoints[childIndex].WorkerToolNames == nil ||
				!slices.Equal(*checkpoints[childIndex].WorkerToolNames, *child.WorkerToolNames) {
				t.Fatalf("compatible child lost its restored scope: %+v", checkpoints)
			}

			cfg := reg.WorkerConfiguration()
			// Keep the stale eligibility name to exercise disappearance of the
			// actual tool, independently of a host disabling scope selection.
			cfg.SubAgentTools = []tool.BaseTool{scopedHTTPTestTool("file_write")}
			reg.ConfigureWorkers(cfg)
			recovery := swarm.RestoredWorker{ID: saved.ID, Role: saved.Role, Task: "continue", Generation: 2,
				ModelBinding: saved.ModelBinding, WorkerToolNames: saved.WorkerToolNames}
			if _, err := reg.Restore(t.Context(), recovery, nil); err == nil ||
				!strings.Contains(err.Error(), "restore worker tool selection: unknown or unavailable worker tool") {
				t.Fatalf("missing saved tool did not block recovery: %v", err)
			}
			after := reg.SnapshotWorkers()
			savedIndex := slices.IndexFunc(after, func(worker swarm.WorkerCheckpoint) bool { return worker.ID == saved.ID })
			if savedIndex < 0 || after[savedIndex].Generation != saved.Generation || after[savedIndex].WorkerToolNames == nil ||
				!slices.Equal(*after[savedIndex].WorkerToolNames, *saved.WorkerToolNames) {
				t.Fatalf("missing saved tool changed the original checkpoint: %+v", after)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if !slices.Equal(fixture.models, []string{"worker-one"}) {
				t.Fatalf("refused Restore reached a model: %v", fixture.models)
			}
		})
	}
}

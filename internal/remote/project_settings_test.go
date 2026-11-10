package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// The phone must edit the PC's real project, not an independent phone copy.
func TestPhoneCreatesAndEditsTheSameProjectAsDesktop(t *testing.T) {
	e := testEngine(t)
	call := func(raw string) Response {
		t.Helper()
		var req Request
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatal(err)
		}
		return Handle(e, config.RemoteConfig{}, req, "relay", "s")
	}
	r := call(`{"op":"project_create","project_patch":{"name":"Phone project","system_prompt":"Original instruction","memory_enabled":true}}`)
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	ps, err := e.ListProjects()
	if err != nil || len(ps) != 1 {
		t.Fatalf("projects: %v %v", ps, err)
	}
	id := ps[0].ID
	r = call(`{"op":"project_update","project_id":"` + id + `","project_patch":{"name":"Renamed"}}`)
	if !r.OK {
		t.Fatalf("update: %+v", r)
	}
	p, _ := e.GetProject(id)
	if p.Name != "Renamed" || p.SystemPrompt != "Original instruction" || !p.MemoryEnabled {
		t.Fatalf("saved: %+v", p)
	}
	r = call(`{"op":"project_get","project_id":"` + id + `"}`)
	raw, _ := json.Marshal(r)
	if !r.OK || !strings.Contains(string(raw), `"system_prompt":"Original instruction"`) {
		t.Fatalf("get: %s", raw)
	}
}

func TestPhoneProjectRefusalsDoNotMutateSettings(t *testing.T) {
	e := testEngine(t)
	p, err := e.CreateProject("Before", "Keep instruction", "", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []string{
		`null`, `{}`, `{"unknown":true}`, `{"name":"After","nuwa_planner_model":"partial"}`,
		`{"name":"After","nuwa_enabled":true,"nuwa_planner_provider_id":"missing","nuwa_planner_model":"missing","nuwa_executor_provider_id":"missing","nuwa_executor_model":"missing"}`, `{"name":"After","workdir":"/missing-phone-project-dir"}`,
		`{"name":"` + strings.Repeat("x", 65000) + `"}`,
	} {
		var req Request
		if err := json.Unmarshal([]byte(`{"op":"project_update","project_id":"`+p.ID+`","project_patch":`+patch+`}`), &req); err != nil {
			t.Fatal(err)
		}
		r := Handle(e, config.RemoteConfig{}, req, "direct", "s")
		if r.OK {
			t.Fatalf("accepted invalid settings: %s", patch[:min(len(patch), 100)])
		}
		got, _ := e.GetProject(p.ID)
		if got.Name != p.Name || got.NuwaEnabled || got.SystemPrompt != p.SystemPrompt {
			t.Fatalf("mutated on refusal: %+v", got)
		}
	}
	// The selected path retains whitespace and cannot drift to its trimmed sibling.
	dir := filepath.Join(t.TempDir(), "chosen ")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"op": "project_update", "project_id": p.ID, "project_patch": map[string]any{"workdir": dir}})
	var req Request
	_ = json.Unmarshal(raw, &req)
	if r := Handle(e, config.RemoteConfig{}, req, "direct", "s"); !r.OK {
		t.Fatalf("path: %+v", r)
	}
	got, _ := e.GetProject(p.ID)
	if got.Workdir != dir {
		t.Fatalf("path changed: %q", got.Workdir)
	}
	if r := Handle(e, config.RemoteConfig{}, Request{Op: "project_get", ProjectID: "missing"}, "direct", "s"); r.OK || r.Code != "not_found" {
		t.Fatalf("missing: %+v", r)
	}
}

func TestPhoneProjectModelsDefaultsAndFrameLimits(t *testing.T) {
	e := testEngine(t)
	call := func(op, id string, patch any) Response {
		t.Helper()
		raw, err := json.Marshal(patch)
		if err != nil {
			t.Fatal(err)
		}
		return Handle(e, config.RemoteConfig{}, Request{Op: op, ProjectID: id, ProjectPatch: raw}, "relay", "s")
	}
	r := call("project_create", "", map[string]any{"name": "With pair", "nuwa_enabled": true, "system_prompt": "initial", "memory_enabled": false})
	if !r.OK || len(r.Projects) != 1 {
		t.Fatalf("create: %+v", r)
	}
	id := r.Projects[0].ID
	p, _ := e.GetProject(id)
	if p.NuwaPlannerProviderID == "" || p.NuwaExecutorModel == "" || p.MemoryEnabled {
		t.Fatalf("pair/default: %+v", p)
	}
	pair := map[string]any{"nuwa_planner_provider_id": p.NuwaPlannerProviderID, "nuwa_planner_model": p.NuwaPlannerModel, "nuwa_executor_provider_id": p.NuwaExecutorProviderID, "nuwa_executor_model": p.NuwaExecutorModel, "nuwa_enabled": false}
	if r := call(" project_update ", id, pair); !r.OK {
		t.Fatalf("pair update: %+v", r)
	}
	p, _ = e.GetProject(id)
	if p.NuwaEnabled || p.NuwaPlannerProviderID == "" {
		t.Fatalf("disabled lost pair: %+v", p)
	}
	for _, patch := range []any{map[string]any{"workdir": "/does-not-exist"}, map[string]any{"memory_enabled": true}} {
		r := call("project_create", "", patch)
		if r.OK || r.Code != "bad_request" {
			t.Fatalf("name required: %+v", r)
		}
	}
	r = call("project_create", "", map[string]any{"name": "Default"})
	if !r.OK {
		t.Fatalf("default: %+v", r)
	}
	r = call("project_create", "", map[string]any{"name": "Bad path", "workdir": "/missing-phone-project-dir"})
	if r.OK || r.Code != "workdir" {
		t.Fatalf("invalid path: %+v", r)
	}
	r = Handle(e, config.RemoteConfig{}, Request{Op: "project_update", ProjectID: id, ProjectPatch: json.RawMessage(`{"name":"Changed"} {}`)}, "relay", "s")
	if r.OK || r.Code != "bad_request" {
		t.Fatalf("trailing: %+v", r)
	}
	r = Handle(e, config.RemoteConfig{}, Request{Op: "project_update", ProjectID: id, ProjectPatch: json.RawMessage(`{"name":`)}, "relay", "s")
	if r.OK {
		t.Fatalf("malformed: %+v", r)
	}
	huge := strings.Repeat("long", 20000)
	if _, err := e.UpdateProject(id, nil, &huge, nil, nil); err != nil {
		t.Fatal(err)
	}
	r = call("project_get", id, nil)
	if r.OK || r.Code != "too_large" {
		t.Fatalf("unbounded read: %+v", r)
	}
	// A name-only write still acknowledges successfully when old data is large.
	if r := call("project_update", id, map[string]any{"name": "Small reply"}); !r.OK {
		t.Fatalf("ack: %+v", r)
	}
	if r := call("project_update", "missing", map[string]any{"name": "No"}); r.OK || r.Code != "not_found" {
		t.Fatalf("missing update: %+v", r)
	}
}

func TestPhoneProjectRPCTraversesTheSealedLinkDispatcher(t *testing.T) {
	e := testEngine(t)
	p, replies := catalogPump(t, e)
	p.Dispatch([]byte(`{"v":1,"id":"create-project","op":"project_create","project_patch":{"name":"From phone","nuwa_enabled":true}}`))
	r := takeCatalogReply(t, replies)
	if !r.OK || len(r.Projects) != 1 || r.ID != "create-project" {
		t.Fatalf("wire create: %+v", r)
	}
	id := r.Projects[0].ID
	raw, _ := json.Marshal(map[string]any{"v": 1, "id": "get-project", "op": "project_get", "project_id": id})
	p.Dispatch(raw)
	r = takeCatalogReply(t, replies)
	if !r.OK || r.Project == nil || !r.Project.NuwaEnabled || r.MemoryAvailable == nil {
		t.Fatalf("wire get: %+v", r)
	}
}

func TestPhoneCatalogReportsExplicitlyDisabledComposerPredictions(t *testing.T) {
	e := testEngine(t)
	if err := e.UpdateConfig(func(cfg *config.Config) error { cfg.Swarm.ComposerPredictions = false; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{OpCatalog, OpRefreshCatalog} {
		r := Handle(e, config.RemoteConfig{}, Request{Op: op}, "relay", "s")
		raw, _ := json.Marshal(r)
		if !r.OK || !strings.Contains(string(raw), `"composer_predictions":false`) {
			t.Fatalf("%s missing explicit false: %s", op, raw)
		}
	}
}

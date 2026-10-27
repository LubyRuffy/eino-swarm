package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func advertised(t *testing.T, bt tool.BaseTool) (map[string]any, []any) {
	t.Helper()
	info, err := bt.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(js)
	var doc struct {
		Properties map[string]any `json:"properties"`
		Required   []any          `json:"required"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Properties, doc.Required
}

// A worker that knew the host's home directory but not the workspace passed
// home as base_dir and searched the whole disk; the parameter was also resent
// on every model call, once per workspace tool. The tools the model sees must
// not offer it, while a call that still sends it (an older transcript) works.
func TestWorkspaceToolsDoNotOfferABaseDirectoryToGuess(t *testing.T) {
	ws := t.TempDir()
	set, err := Build(context.Background(), configFor(t), ws)
	if err != nil {
		t.Fatal(err)
	}
	hidden := 0
	for i, name := range set.Names {
		props, _ := advertised(t, set.Tools[i])
		if _, ok := props[hiddenBaseDir]; ok {
			t.Fatalf("%s still advertises %s", name, hiddenBaseDir)
		}
		if name == "read" {
			if _, ok := props["file_path"]; !ok {
				t.Fatalf("read lost its own parameters: %v", props)
			}
			hidden++
		}
	}
	if hidden == 0 {
		t.Fatal("the default toolset has no read tool to check")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "kept.txt"), []byte("still readable"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"file_path": "kept.txt", "base_dir": other})
	out, err := find(t, set, "read").InvokableRun(context.Background(), string(args))
	if err != nil || !strings.Contains(out, "still readable") {
		t.Fatalf("a call that still sends base_dir must keep working: out=%q err=%v", out, err)
	}
}

func TestWithoutParamCopiesBothSchemaForms(t *testing.T) {
	byParams := &schema.ToolInfo{Name: "p", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"base_dir": {Type: schema.String, Required: true},
		"path":     {Type: schema.String, Required: true},
	})}
	got := withoutParam(byParams, hiddenBaseDir)
	props, req := advertised(t, &stubInfo{info: got})
	if _, ok := props["base_dir"]; ok || props["path"] == nil || len(req) != 1 || req[0] != "path" {
		t.Fatalf("params form: props=%v required=%v", props, req)
	}
	if props, _ := advertised(t, &stubInfo{info: byParams}); props["base_dir"] == nil {
		t.Fatal("the shared upstream ToolInfo was modified")
	}

	js, err := byParams.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	bySchema := &schema.ToolInfo{Name: "j", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(js)}
	props, req = advertised(t, &stubInfo{info: withoutParam(bySchema, hiddenBaseDir)})
	if _, ok := props["base_dir"]; ok || props["path"] == nil || len(req) != 1 || req[0] != "path" {
		t.Fatalf("json schema form: props=%v required=%v", props, req)
	}

	unencodable := &schema.ToolInfo{Name: "x", Extra: map[string]any{"f": func() {}}, ParamsOneOf: byParams.ParamsOneOf}
	if withoutParam(unencodable, hiddenBaseDir) != unencodable {
		t.Fatal("a ToolInfo JSON cannot encode must be advertised unchanged")
	}
	plain := &schema.ToolInfo{Name: "none"}
	if withoutParam(plain, hiddenBaseDir) != plain {
		t.Fatal("a tool without the parameter must be advertised unchanged")
	}
	broken := &stubInfo{err: errors.New("no info")}
	if _, err := wrapAdvertisedParams(broken).Info(context.Background()); err == nil {
		t.Fatal("an upstream Info error must pass through")
	}
	if wrapAdvertisedParams(infoOnlyTool{}) != (infoOnlyTool{}) {
		t.Fatal("a non-invokable tool is returned unchanged")
	}
}

type stubInfo struct {
	info *schema.ToolInfo
	err  error
}

func (s *stubInfo) Info(context.Context) (*schema.ToolInfo, error) { return s.info, s.err }

func (s *stubInfo) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", nil
}

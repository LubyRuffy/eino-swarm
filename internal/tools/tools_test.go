package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
)

func TestCatalogIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Catalog() {
		if d.Name == "" || d.Title == "" || d.Summary == "" || d.Group == "" {
			t.Fatalf("incomplete descriptor: %+v", d)
		}
		if seen[d.Name] {
			t.Fatalf("duplicate tool %q", d.Name)
		}
		seen[d.Name] = true
	}
	if len(seen) < 10 {
		t.Fatalf("catalog looks truncated: %d entries", len(seen))
	}
	// the returned slice must be a copy, or a caller can corrupt the catalog
	c := Catalog()
	c[0].Name = "mutated"
	if Catalog()[0].Name == "mutated" {
		t.Fatal("Catalog returns the backing array")
	}
}

// Every catalog entry must be constructible; a descriptor with no constructor
// would only fail once a user toggled it on.
func TestEveryCatalogToolBuilds(t *testing.T) {
	cfg := configFor(t)
	var all []string
	for _, d := range Catalog() {
		if d.DefaultOff {
			all = append(all, d.Name)
		}
	}
	cfg.Tools.Enabled = all

	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(set.Tools) != len(Catalog()) {
		t.Fatalf("built %d tools, catalog has %d", len(set.Tools), len(Catalog()))
	}
	for i, bt := range set.Tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatalf("tool %d Info: %v", i, err)
		}
		if info.Name != set.Names[i] {
			t.Fatalf("Names[%d]=%q but the tool reports %q", i, set.Names[i], info.Name)
		}
	}
}

func TestDefaultsAreOnExceptOptInTools(t *testing.T) {
	cfg := configFor(t)
	names := Enabled(cfg.Tools)
	for _, d := range Catalog() {
		got := containsStr(names, d.Name)
		if d.DefaultOff && got {
			t.Fatalf("%q needs an external dependency and must be opt-in", d.Name)
		}
		if !d.DefaultOff && !got {
			t.Fatalf("%q should be on by default", d.Name)
		}
	}
}

func TestToggleTools(t *testing.T) {
	cfg := configFor(t)
	cfg.Tools.Disabled = []string{"exec", "web_search"}
	cfg.Tools.Enabled = []string{"python_runner"}

	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if containsStr(set.Names, "exec") || containsStr(set.Names, "web_search") {
		t.Fatalf("disabled tools were built: %v", set.Names)
	}
	if !containsStr(set.Names, "python_runner") {
		t.Fatalf("explicitly enabled tool missing: %v", set.Names)
	}
	if !containsStr(set.Names, "read") {
		t.Fatalf("untouched tool went missing: %v", set.Names)
	}
}

// The workspace has to exist before the tools are built: they stat their base
// directory, so a missing one turns into a confusing failure on every call.
func TestBuildCreatesWorkspace(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "nested", "workspace")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if set.WorkspaceDir != ws {
		t.Fatalf("WorkspaceDir=%q", set.WorkspaceDir)
	}
	info, err := os.Stat(ws)
	if err != nil || !info.IsDir() {
		t.Fatalf("workspace not created: %v", err)
	}
}

func TestBuildRejectsUnusableWorkspace(t *testing.T) {
	cfg := configFor(t)
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), cfg, filepath.Join(file, "ws")); err == nil {
		t.Fatal("want an error when the workspace path cannot be created")
	}
}

// Relative paths must land in the conversation's workspace, which is what
// keeps two conversations' files apart and makes the Files panel correct.
func TestRelativePathsResolveInsideTheWorkspace(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	writeTool := find(t, set, "write")
	args, _ := json.Marshal(map[string]string{
		"file_path": "notes/report.md",
		"content":   "hello from the tool layer",
	})
	if _, err := writeTool.InvokableRun(context.Background(), string(args)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(ws, "notes", "report.md"))
	if err != nil {
		t.Fatalf("file did not land in the workspace: %v", err)
	}
	if string(got) != "hello from the tool layer" {
		t.Fatalf("content=%q", got)
	}

	readTool := find(t, set, "read")
	rargs, _ := json.Marshal(map[string]string{"file_path": "notes/report.md"})
	out, err := readTool.InvokableRun(context.Background(), string(rargs))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !contains(out, "hello from the tool layer") {
		t.Fatalf("read did not return the file: %q", out)
	}
}

func TestProxyConfigReachesNetworkTools(t *testing.T) {
	cfg := configFor(t)
	cfg.Tools.Proxy = config.ProxyConfig{
		HTTP:    "http://127.0.0.1:7890",
		HTTPS:   "http://127.0.0.1:7890",
		NoProxy: "localhost",
	}
	// A bad proxy must not stop the toolset from assembling: it should only
	// affect the calls that use it, and only when they run.
	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatalf("Build with a proxy: %v", err)
	}
	if !containsStr(set.Names, "web_fetch") || !containsStr(set.Names, "web_search") {
		t.Fatalf("network tools missing: %v", set.Names)
	}
}

// ---------- helpers ----------

func configFor(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func find(t *testing.T, set *Set, name string) tool.InvokableTool {
	t.Helper()
	for i, n := range set.Names {
		if n != name {
			continue
		}
		it, ok := set.Tools[i].(tool.InvokableTool)
		if !ok {
			t.Fatalf("tool %q is not invokable", name)
		}
		return it
	}
	t.Fatalf("tool %q not built", name)
	return nil
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Compaction may clear catalog tool results; it must not treat a missing
// name, or a lifecycle/memory tool, as something it can re-fetch.
func TestReplayableResultIsTheCatalog(t *testing.T) {
	if ReplayableResult("") || ReplayableResult("spawn_agent") || ReplayableResult("memory") {
		t.Fatal("non-catalog tools must keep their results")
	}
	name := Catalog()[0].Name
	if !ReplayableResult(name) {
		t.Fatalf("%q is in the catalog and must be replayable", name)
	}
}

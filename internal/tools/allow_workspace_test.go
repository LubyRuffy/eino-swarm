package tools

import "testing"

func TestAllowWorkspaceKeepsOnlyTheNamedCatalogTools(t *testing.T) {
	if AllowWorkspace(nil, []string{"read"}) != nil {
		t.Fatal("nil set was replaced")
	}
	in := &Set{WorkspaceDir: "/work", Names: []string{"read", "exec", "mcp_remote"}, Tools: nil}
	out := AllowWorkspace(in, []string{"read", "write"})
	if len(out.Names) != 1 || out.Names[0] != "read" || out.WorkspaceDir != "/work" {
		t.Fatalf("allowlist = %v", out.Names)
	}
	if len(in.Names) != 3 {
		t.Fatal("allowlist mutated the original set")
	}
	empty := AllowWorkspace(in, nil)
	if len(empty.Names) != 0 {
		t.Fatalf("empty allowlist kept %v", empty.Names)
	}
	if !KnownWorkspaceTool("read") || !KnownWorkspaceTool("screenshot") || KnownWorkspaceTool("spawn_agent") || KnownWorkspaceTool(" ") {
		t.Fatal("workspace tool names were not recognized")
	}
}

package tools

import (
	"strings"

	"github.com/LubyRuffy/eino-tools/edit"
	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/LubyRuffy/eino-tools/pythonrunner"
	"github.com/LubyRuffy/eino-tools/write"
)

// MutatingCatalogNames are workspace tools that change files or run
// commands. Planning unmounts them instead of pretending the workspace is
// confined.
func MutatingCatalogNames() []string {
	return []string{
		write.ToolName,
		edit.ToolName,
		einoexec.ToolName,
		pythonrunner.ToolName,
	}
}

// AllowWorkspace keeps catalog tools named in allow and drops every other
// workspace or MCP tool already on the set. Coordination tools are added
// later, so they are not part of this cut. A name the local switches already
// removed stays removed.
func AllowWorkspace(set *Set, allow []string) *Set {
	if set == nil {
		return nil
	}
	keep := make(map[string]bool, len(allow))
	for _, name := range allow {
		keep[name] = true
	}
	out := &Set{WorkspaceDir: set.WorkspaceDir}
	for i, name := range set.Names {
		if !keep[name] || strings.HasPrefix(name, "mcp_") {
			continue
		}
		out.Names = append(out.Names, name)
		if i < len(set.Tools) {
			out.Tools = append(out.Tools, set.Tools[i])
		}
	}
	return out
}

// ExploreOnly returns a copy of set with mutating catalog tools removed.
// Read, search, and web tools stay. Nil in, nil out.
func ExploreOnly(set *Set) *Set {
	if set == nil {
		return nil
	}
	drop := map[string]bool{}
	for _, name := range MutatingCatalogNames() {
		drop[name] = true
	}
	out := &Set{WorkspaceDir: set.WorkspaceDir}
	for i, name := range set.Names {
		// Remote annotations are hints, not proof that an external tool cannot write.
		if drop[name] || strings.HasPrefix(name, "mcp_") {
			continue
		}
		out.Names = append(out.Names, name)
		if i < len(set.Tools) {
			out.Tools = append(out.Tools, set.Tools[i])
		}
	}
	return out
}

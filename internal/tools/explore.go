package tools

import (
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
		if drop[name] {
			continue
		}
		out.Names = append(out.Names, name)
		if i < len(set.Tools) {
			out.Tools = append(out.Tools, set.Tools[i])
		}
	}
	return out
}

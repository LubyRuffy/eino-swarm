package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// A provider that caches by prefix can serve a new conversation's first call
// from another conversation's, but only up to the first byte that differs. The
// workspace path used to sit mid-prompt, so every new conversation re-read the
// second half of the system prompt at full price (4480 of 6909 prompt tokens
// were cached on a first call, against 80% or more on later calls). The path now
// follows all static text, so the shared prefix is the whole generic prompt.
func TestManagerPromptSharesItsPrefixAcrossConversations(t *testing.T) {
	cfg := config.Default()
	a := ManagerPrompt(&tools.Set{WorkspaceDir: "/data/ws/th_aaaaaaaaaaaaaaaa", Names: []string{"exec"}}, cfg, "")
	b := ManagerPrompt(&tools.Set{WorkspaceDir: "/data/ws/th_bbbbbbbbbbbbbbbb", Names: []string{"exec"}}, cfg, "")

	shared := 0
	for shared < len(a) && shared < len(b) && a[shared] == b[shared] {
		shared++
	}
	marker := strings.Index(a, "## Workspace path")
	if marker < 0 {
		t.Fatal("the prompt has no Workspace path section")
	}
	if shared < marker {
		t.Fatalf("conversation-specific text precedes the workspace section at byte %d (section starts at %d):\n%s", shared, marker, a[max(0, shared-80):shared+80])
	}
	// The generic text must still teach what every later section assumes.
	for _, section := range []string{"## Delegating", "## Working files", "## Asking the human", "## Waiting", "## Answering"} {
		if i := strings.Index(a, section); i < 0 || i > marker {
			t.Fatalf("%q must precede the per-conversation workspace path (at %d, path section at %d)", section, i, marker)
		}
	}
	if strings.Count(a, "/data/ws/th_aaaaaaaaaaaaaaaa") != 1 {
		t.Fatalf("the workspace path should appear exactly once, after the static text:\n%s", a[marker:])
	}
}

func TestManagerPromptKeepsProjectTextAfterTheWorkspacePath(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/data/ws/th_x"}, config.Default(), "## This project\n\nBe terse.\n")
	path := strings.Index(prompt, "## Workspace path")
	project := strings.Index(prompt, "## This project")
	if path < 0 || project < path {
		t.Fatalf("project text must be the last thing the model reads (path at %d, project at %d)", path, project)
	}
}

func TestManagerPromptWithoutAWorkspaceHasNoEmptyPathSection(t *testing.T) {
	if prompt := ManagerPrompt(&tools.Set{}, config.Default(), ""); strings.Contains(prompt, "## Workspace path") {
		t.Fatal("an empty workspace must not invent a path section")
	}
}

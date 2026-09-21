// Package tools assembles the agents' toolset from github.com/LubyRuffy/eino-tools,
// anchored at one conversation's workspace directory.
//
// # Access model
//
// zwai runs its agents with full access, like Codex's "Full access" mode: the
// workspace is the directory relative paths resolve against and the one the
// Files panel shows, but the tools themselves do not confine agents to it —
// an absolute path or a `..` walks out, and `exec` runs arbitrary commands as
// the current user. That is deliberate for a co-working assistant, and it is
// why the UI labels the mode plainly. Do not describe the workspace as a
// sandbox anywhere: it is an anchor, not a boundary.
package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-tools/edit"
	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/LubyRuffy/eino-tools/glob"
	"github.com/LubyRuffy/eino-tools/grep"
	"github.com/LubyRuffy/eino-tools/ls"
	"github.com/LubyRuffy/eino-tools/netproxy"
	"github.com/LubyRuffy/eino-tools/pythonrunner"
	"github.com/LubyRuffy/eino-tools/read"
	"github.com/LubyRuffy/eino-tools/screenshot"
	"github.com/LubyRuffy/eino-tools/tree"
	"github.com/LubyRuffy/eino-tools/webfetch"
	"github.com/LubyRuffy/eino-tools/websearch"
	"github.com/LubyRuffy/eino-tools/write"
	"github.com/cloudwego/eino/components/tool"
)

// Tool groups, used by the settings UI to lay the toggles out.
const (
	GroupFiles = "files"
	GroupShell = "shell"
	GroupWeb   = "web"
)

// Descriptor describes one tool to the settings UI.
type Descriptor struct {
	Name string `json:"name"`
	// Title is the human label; Summary is one line of explanation.
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Group   string `json:"group"`
	// Network is true for tools that reach the internet, so the UI can show
	// which ones the proxy settings apply to.
	Network bool `json:"network"`
	// DefaultOff marks tools that need something zwai does not ship (a Python
	// interpreter, a headless browser) and are therefore opt-in.
	DefaultOff bool `json:"default_off"`
}

// catalog is the full set of tools this build knows how to construct.
var catalog = []Descriptor{
	{Name: read.ToolName, Title: "Read file", Summary: "Read a text file, with paging for large ones.", Group: GroupFiles},
	{Name: write.ToolName, Title: "Write file", Summary: "Create or overwrite a file.", Group: GroupFiles},
	{Name: edit.ToolName, Title: "Edit file", Summary: "Apply search/replace or patch edits to a file.", Group: GroupFiles},
	{Name: ls.ToolName, Title: "List directory", Summary: "List files and directories.", Group: GroupFiles},
	{Name: tree.ToolName, Title: "Directory tree", Summary: "Show a directory tree with depth control.", Group: GroupFiles},
	{Name: glob.ToolName, Title: "Find by pattern", Summary: "Match files with glob patterns.", Group: GroupFiles},
	{Name: grep.ToolName, Title: "Search in files", Summary: "Search file contents for a pattern.", Group: GroupFiles},
	{Name: einoexec.ToolName, Title: "Run command", Summary: "Run a shell command, with pipes and redirects.", Group: GroupShell},
	{Name: websearch.ToolName, Title: "Web search", Summary: "Search the web.", Group: GroupWeb, Network: true},
	{Name: webfetch.ToolName, Title: "Fetch page", Summary: "Fetch a web page and extract its text.", Group: GroupWeb, Network: true},
	{Name: pythonrunner.ToolName, Title: "Run Python", Summary: "Run a Python snippet. Needs Python on PATH.", Group: GroupShell, DefaultOff: true},
	{Name: screenshot.ToolName, Title: "Screenshot", Summary: "Capture a screenshot. Needs platform capture support.", Group: GroupShell, DefaultOff: true},
}

// ReplayableResult is a tool whose output is a file body, a listing, a
// search hit or a command dump — something the agent can fetch again.
// Compaction may replace older results with a placeholder. Lifecycle and
// memory tools are not in the catalog, so they stay intact.
func ReplayableResult(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, d := range catalog {
		if d.Name == name {
			return true
		}
	}
	return false
}

// Catalog returns every tool this build can construct, in a stable order.
func Catalog() []Descriptor {
	out := make([]Descriptor, len(catalog))
	copy(out, catalog)
	return out
}

// Enabled reports which catalog entries cfg turns on.
func Enabled(cfg config.ToolsConfig) []string {
	var out []string
	for _, d := range catalog {
		if active(cfg, d) {
			out = append(out, d.Name)
		}
	}
	sort.Strings(out)
	return out
}

func active(cfg config.ToolsConfig, d Descriptor) bool {
	if d.DefaultOff {
		return cfg.IsEnabled(d.Name)
	}
	return !cfg.IsDisabled(d.Name)
}

// Set is one conversation's assembled toolset.
type Set struct {
	// Tools is what gets registered on the agents.
	Tools []tool.BaseTool
	// Names lists the tools in Tools, for the system prompt and for tracing.
	Names []string
	// WorkspaceDir is the directory relative paths resolve against.
	WorkspaceDir string
}

// Build assembles the toolset for one conversation.
//
// workspaceDir must exist: the underlying tools stat their base directory and
// refuse to run against a missing one, which would otherwise surface as a
// confusing per-tool-call error instead of a startup failure.
func Build(ctx context.Context, cfg *config.Config, workspaceDir string) (*Set, error) {
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		return nil, fmt.Errorf("tools: create workspace %s: %w", workspaceDir, err)
	}

	proxy := netproxy.Config{
		HTTPProxy:  cfg.Tools.Proxy.HTTP,
		HTTPSProxy: cfg.Tools.Proxy.HTTPS,
		NoProxy:    cfg.Tools.Proxy.NoProxy,
	}

	// One constructor per catalog entry, so adding a tool is a one-line change
	// in two places that the tests then hold together.
	builders := map[string]func() (tool.BaseTool, error){
		read.ToolName:  func() (tool.BaseTool, error) { return read.New(read.Config{DefaultBaseDir: workspaceDir}) },
		write.ToolName: func() (tool.BaseTool, error) { return write.New(write.Config{DefaultBaseDir: workspaceDir}) },
		edit.ToolName:  func() (tool.BaseTool, error) { return edit.New(edit.Config{DefaultBaseDir: workspaceDir}) },
		ls.ToolName:    func() (tool.BaseTool, error) { return ls.New(ls.Config{DefaultBaseDir: workspaceDir}) },
		tree.ToolName:  func() (tool.BaseTool, error) { return tree.New(tree.Config{DefaultBaseDir: workspaceDir}) },
		glob.ToolName:  func() (tool.BaseTool, error) { return glob.New(glob.Config{DefaultBaseDir: workspaceDir}) },
		grep.ToolName:  func() (tool.BaseTool, error) { return grep.New(grep.Config{DefaultBaseDir: workspaceDir}) },
		einoexec.ToolName: func() (tool.BaseTool, error) {
			inner, err := einoexec.New(einoexec.Config{DefaultBaseDir: workspaceDir})
			if err != nil {
				return nil, err
			}
			return wrapExecSleepBias(inner)
		},
		websearch.ToolName: func() (tool.BaseTool, error) {
			return websearch.New(ctx, websearch.Config{
				ProxyConfig: proxy,
				MaxResults:  cfg.Tools.WebSearchMaxResults,
			})
		},
		webfetch.ToolName: func() (tool.BaseTool, error) {
			return webfetch.New(webfetch.Config{ProxyConfig: proxy})
		},
		pythonrunner.ToolName: func() (tool.BaseTool, error) {
			return pythonrunner.New(pythonrunner.Config{})
		},
		screenshot.ToolName: func() (tool.BaseTool, error) {
			return screenshot.New(screenshot.Config{DefaultBaseDir: workspaceDir})
		},
	}

	set := &Set{WorkspaceDir: workspaceDir}
	for _, d := range catalog {
		if !active(cfg.Tools, d) {
			continue
		}
		build, ok := builders[d.Name]
		if !ok {
			return nil, fmt.Errorf("tools: %q is in the catalog but has no constructor", d.Name)
		}
		t, err := build()
		if err != nil {
			return nil, fmt.Errorf("tools: build %q: %w", d.Name, err)
		}
		set.Tools = append(set.Tools, t)
		set.Names = append(set.Names, d.Name)
	}
	return set, nil
}

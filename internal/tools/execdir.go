package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// execWorkDir refuses a missing working directory before the shell starts.
// On macOS a process-group spawn into a missing directory fails as
// "fork/exec <shell>: no such file or directory", which names the shell
// instead of the directory; a model reading that concludes the shell is gone
// and stops using exec for the rest of the conversation. The refusal is a
// failed result, not a Go error: nothing between a tool and the model turns
// an error into a result, so an error would end the whole turn.
type execWorkDir struct {
	inner        tool.InvokableTool
	workspaceDir string
}

func wrapExecWorkDir(inner tool.InvokableTool, workspaceDir string) tool.InvokableTool {
	return &execWorkDir{inner: inner, workspaceDir: workspaceDir}
}

func (g *execWorkDir) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

func (g *execWorkDir) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	if err := missingExecWorkDir(args, g.workspaceDir); err != nil {
		return refusedExecResult(err), nil
	}
	return g.inner.InvokableRun(ctx, args, opts...)
}

// refusedExecResult uses exec's own failure keys for a command that never
// started, so the model reads an ordinary failed call it can correct.
func refusedExecResult(err error) string {
	out, _ := json.Marshal(map[string]any{
		"exit_code": -1, "stdout": "", "stderr": "", "failed": true, "error": err.Error(),
	})
	return string(out)
}

// missingExecWorkDir resolves cwd the way exec does: an absolute cwd as
// given, a relative one under base_dir or else the workspace. Malformed
// arguments and base_dir problems are left to exec, which already reports
// them by name.
func missingExecWorkDir(args, workspaceDir string) error {
	var p struct {
		CWD     string `json:"cwd"`
		BaseDir string `json:"base_dir"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return nil
	}
	cwd := strings.TrimSpace(p.CWD)
	if cwd == "" {
		return nil
	}
	base := strings.TrimSpace(p.BaseDir)
	if base == "" {
		base = strings.TrimSpace(workspaceDir)
	}
	dir := cwd
	if !filepath.IsAbs(cwd) {
		if base == "" {
			return nil
		}
		dir = filepath.Join(base, cwd)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("exec: cwd %s is not a directory; the command did not run", dir)
	case os.IsNotExist(err):
		return fmt.Errorf("exec: cwd %s does not exist; the command did not run. Omit cwd to run in the workspace", dir)
	default:
		return fmt.Errorf("exec: cwd %s is not accessible: %v; the command did not run", dir, err)
	}
}

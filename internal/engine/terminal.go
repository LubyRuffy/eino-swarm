package engine

import (
	"fmt"
	"os"
	"strings"
)

// TerminalDir is where a human terminal for this conversation or project
// starts. The client names the conversation or the project; it does not
// name a path. A conversation in a project shares that project's directory,
// the same way the agents do — otherwise the shell and the tools would
// silently edit different trees.
func (e *Engine) TerminalDir(threadID, projectID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	projectID = strings.TrimSpace(projectID)
	var dir string
	switch {
	case threadID != "":
		if _, err := e.store.GetThread(threadID); err != nil {
			return "", err
		}
		dir = e.WorkspaceDir(threadID)
	case projectID != "":
		p, err := e.store.GetProject(projectID)
		if err != nil {
			return "", err
		}
		dir = e.ProjectWorkdir(p)
	default:
		return "", fmt.Errorf("engine: a conversation or a project is required")
	}
	return ensureTerminalDir(e, dir)
}

func ensureTerminalDir(e *Engine, dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) && e.ownsWorkspace(dir) {
			// A scratch workspace zwai created can be rebuilt; a path the
			// user typed must not, or a typo becomes a new empty tree.
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", fmt.Errorf("engine: create workspace: %w", err)
			}
			return dir, nil
		}
		return "", fmt.Errorf("engine: working directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("engine: working directory %s is not a directory", dir)
	}
	return dir, nil
}

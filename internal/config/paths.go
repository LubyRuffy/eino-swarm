package config

import "path/filepath"

// Directory names under the data directory. They are also what tells a
// managed directory apart from a path the user chose: deleting a conversation
// may remove a directory under WorkspacesDir, and never one outside it.
const (
	workspacesDirName = "workspaces"
	projectsDirName   = "projects"
	// Pasted images live here, not in the workspace: a project pointed at a
	// repository must not grow screenshot files, and vision input is not a
	// working file.
	inputsDirName = "inputs"
	plansDirName  = "plans"
	planFileName  = "PLAN.md"
	// A project's own subdirectories: the working directory zwai manages when
	// the user did not name one, and the memory store.
	projectWorkspaceName = "workspace"
	projectMemoryName    = "memory"
)

// WorkspacesDir is the parent of every conversation's own workspace.
func (c *Config) WorkspacesDir() string { return filepath.Join(c.dataDir, workspacesDirName) }

// WorkspaceDir is one conversation's workspace: the directory relative tool
// paths resolve against and the one the Files panel shows. It is an anchor,
// not a boundary — see the access model in internal/tools.
func (c *Config) WorkspaceDir(threadID string) string {
	return filepath.Join(c.WorkspacesDir(), threadID)
}

// InputsDir holds pasted images for every conversation.
func (c *Config) InputsDir() string { return filepath.Join(c.dataDir, inputsDirName) }

// PlansDir holds conversation plan files. They are app artefacts, not
// workspace files, so a project pointed at a repository does not grow them.
func (c *Config) PlansDir() string { return filepath.Join(c.dataDir, plansDirName) }

// ThreadPlanFile is one conversation's PLAN.md.
func (c *Config) ThreadPlanFile(threadID string) string {
	return filepath.Join(c.PlansDir(), threadID, planFileName)
}

// ThreadInputsDir is one conversation's pasted images. Named by thread id so
// deleting the conversation can take them with it without walking the table.
func (c *Config) ThreadInputsDir(threadID string) string {
	return filepath.Join(c.InputsDir(), threadID)
}

// ProjectsDir is the parent of every project's managed directory.
func (c *Config) ProjectsDir() string { return filepath.Join(c.dataDir, projectsDirName) }

// ProjectDir is one project's managed directory.
func (c *Config) ProjectDir(projectID string) string {
	return filepath.Join(c.ProjectsDir(), projectID)
}

// ProjectWorkspaceDir is the working directory of a project that did not name
// one of its own.
func (c *Config) ProjectWorkspaceDir(projectID string) string {
	return filepath.Join(c.ProjectDir(projectID), projectWorkspaceName)
}

// ProjectMemoryDir holds a project's MEMORY.md and its skills.
//
// It sits under the data directory rather than inside the project's working
// directory on purpose: the agents rewrite it after most turns, and a store
// that lived in the user's repository would show up as unexplained changes in
// every `git status`.
func (c *Config) ProjectMemoryDir(projectID string) string {
	return filepath.Join(c.ProjectDir(projectID), projectMemoryName)
}

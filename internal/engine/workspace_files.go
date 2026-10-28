package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// FileEntry describes one file in a conversation's workspace.
type FileEntry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Dir      bool      `json:"dir"`
	Modified time.Time `json:"modified"`
}

// maxWorkspaceEntries bounds the Files panel listing: an agent that generates
// thousands of files must not be able to hang the UI.
const maxWorkspaceEntries = 2000

// ListFiles walks a conversation's workspace, returning workspace-relative
// paths. The walk is breadth-first so a fat directory that sorts early
// cannot hide later siblings from the Files panel.
func (e *Engine) ListFiles(threadID string) ([]FileEntry, error) {
	root := e.WorkspaceDir(threadID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("engine: workspace: %w", err)
	}
	return listWorkspace(root, maxWorkspaceEntries), nil
}

// ResolveWorkspacePath turns a client-supplied relative path into an absolute
// one inside the conversation's workspace, rejecting anything that escapes.
//
// The agents run with full access on purpose, but the HTTP surface must not:
// an unauthenticated download endpoint that accepts `../../../etc/passwd`
// turns a local app into a file server for the whole machine.
//
// A symlink inside the workspace may point anywhere (agents made it), so a
// directory link is never walked through: the path's parent components must
// resolve inside the workspace. The last component is returned as named; the
// callers that read or delete it go through os.Root as well.
func (e *Engine) ResolveWorkspacePath(threadID, rel string) (string, error) {
	name, err := workspaceRelName(rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(e.WorkspaceDir(threadID))
	if err != nil {
		return "", fmt.Errorf("engine: workspace: %w", err)
	}
	abs := filepath.Join(rootAbs, name)
	if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("engine: %q is outside the conversation workspace", rel)
	}
	root, err := os.OpenRoot(rootAbs)
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("engine: workspace: %w", err)
	}
	defer root.Close()
	if _, err := root.Lstat(name); err != nil && !missingPath(err) {
		return "", fmt.Errorf("engine: %q cannot be resolved inside the conversation workspace: %w", rel, err)
	}
	return abs, nil
}

// workspaceRelName validates a client-supplied workspace path and returns it
// relative to the workspace. Traversal is rejected instead of normalized away:
// cleaning "../../x" to "x" would silently serve a different file than the
// client asked for. Surrounding whitespace is part of a file name, so it is
// kept: "notes.md " and "notes.md" are different files.
func workspaceRelName(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("engine: no path given")
	}
	slashed := filepath.ToSlash(rel)
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return "", fmt.Errorf("engine: %q may not walk out of the conversation workspace", rel)
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+slashed), "/")
	if clean == "" {
		return "", fmt.Errorf("engine: no path given")
	}
	return filepath.FromSlash(clean), nil
}

// missingPath reports a lookup that failed only because the path does not
// exist, including a regular file used as a directory component.
func missingPath(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// OpenWorkspaceFile opens a client-named file for reading. A symlink is
// followed only while its target stays inside the workspace.
func (e *Engine) OpenWorkspaceFile(threadID, rel string) (*os.File, error) {
	name, err := workspaceRelName(rel)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(e.WorkspaceDir(threadID))
	if err != nil {
		return nil, fmt.Errorf("engine: workspace: %w", err)
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil && !missingPath(err) {
		return nil, fmt.Errorf("engine: %q cannot be opened inside the conversation workspace: %w", rel, err)
	}
	return f, err
}

// SaveUpload writes an uploaded file into the conversation's workspace and
// records it, returning the workspace-relative path the agents will see.
func (e *Engine) SaveUpload(threadID, name string, read func(dst string) error) (*store.Attachment, error) {
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, err
	}
	base := filepath.Base(filepath.FromSlash(name))
	if base == "." || base == string(os.PathSeparator) || strings.TrimSpace(base) == "" {
		return nil, fmt.Errorf("engine: upload has no usable file name")
	}
	uploads := filepath.Join(e.WorkspaceDir(threadID), uploadsDir)
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		return nil, fmt.Errorf("engine: create uploads dir: %w", err)
	}
	dst := uniquePath(filepath.Join(uploads, base))
	if err := read(dst); err != nil {
		return nil, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return nil, fmt.Errorf("engine: stat upload: %w", err)
	}
	rel, err := filepath.Rel(e.WorkspaceDir(threadID), dst)
	if err != nil {
		return nil, fmt.Errorf("engine: relative upload path: %w", err)
	}
	att := &store.Attachment{
		ThreadID: threadID,
		Name:     filepath.Base(dst),
		RelPath:  filepath.ToSlash(rel),
		Size:     info.Size(),
	}
	if err := e.store.AddAttachment(att); err != nil {
		return nil, err
	}
	_ = e.store.TouchThread(threadID)
	return att, nil
}

// uploadsDir keeps user uploads together so the system prompt can point the
// agents at one place and the Files panel can show provenance.
const uploadsDir = "uploads"

// DeleteFile removes one file from a conversation's workspace. Deleting a
// symlink removes the link, never what it points at.
func (e *Engine) DeleteFile(threadID, rel string) error {
	if _, err := e.ResolveWorkspacePath(threadID, rel); err != nil {
		return err
	}
	name, _ := workspaceRelName(rel)
	root, err := os.OpenRoot(e.WorkspaceDir(threadID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("engine: workspace: %w", err)
	}
	if root != nil {
		defer root.Close()
		if err := root.RemoveAll(name); err != nil && !missingPath(err) {
			return fmt.Errorf("engine: delete %s: %w", rel, err)
		}
	}
	return e.store.DeleteAttachmentByPath(threadID, filepath.ToSlash(name))
}

// uniquePath appends a counter until the path is free, so two uploads with the
// same name do not silently overwrite each other.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext)
}

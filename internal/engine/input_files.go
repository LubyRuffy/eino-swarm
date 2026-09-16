package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Caps on files attached to one send. The workspace can hold more; this is
// how many a single message may name before the model is drowning in paths.
var maxAttachedFiles = 32

func withAttachedFiles(text string, files []store.Attachment) string {
	notice := attachedFilesNotice(files)
	if notice == "" {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return notice
	}
	return strings.TrimRight(text, "\n") + "\n\n" + notice
}

func attachedFilesNotice(files []store.Attachment) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Attached files:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f.RelPath)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (e *Engine) resolveAttachedFiles(threadID string, paths []string) ([]store.Attachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if len(paths) > maxAttachedFiles {
		return nil, fmt.Errorf("engine: at most %d files can be attached to a message", maxAttachedFiles)
	}
	listed, err := e.store.ListAttachments(threadID)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]store.Attachment, len(listed))
	for _, a := range listed {
		byPath[a.RelPath] = a
	}
	seen := map[string]bool{}
	out := make([]store.Attachment, 0, len(paths))
	for _, raw := range paths {
		rel, err := normalizeAttachedPath(raw)
		if err != nil {
			return nil, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		att, ok := byPath[rel]
		if !ok {
			return nil, fmt.Errorf("engine: attached file %q was not uploaded", rel)
		}
		abs, err := e.ResolveWorkspacePath(threadID, rel)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("engine: attached file %q is missing", rel)
		}
		out = append(out, att)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("engine: attached files listed nothing usable")
	}
	return out, nil
}

func normalizeAttachedPath(raw string) (string, error) {
	rel := filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw)))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", fmt.Errorf("engine: attached file %q is not a workspace upload", raw)
	}
	if rel != uploadsDir && !strings.HasPrefix(rel, uploadsDir+"/") {
		return "", fmt.Errorf("engine: attached file %q is not a workspace upload", rel)
	}
	if rel == uploadsDir {
		return "", fmt.Errorf("engine: attached file %q is not a workspace upload", rel)
	}
	return rel, nil
}

func (e *Engine) bindAttachmentTurns(files []store.Attachment, turnID string) error {
	for _, f := range files {
		if err := e.store.BindAttachmentTurn(f.ThreadID, f.RelPath, turnID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) hydrateUserInput(threadID string, in UserInput, emptyErr string) (string, []store.Attachment, error) {
	files, err := e.resolveAttachedFiles(threadID, in.Files)
	if err != nil {
		return "", nil, err
	}
	caption := strings.TrimSpace(in.Text)
	text := withAttachedFiles(caption, files)
	// A rewind with no new pixels still has the original images; StartTurn
	// checks those after peekRewind so a blank edit cannot wipe the log.
	if text == "" && len(in.Images) == 0 && in.FromEventSeq <= 0 {
		return "", nil, fmt.Errorf("%s", emptyErr)
	}
	return text, files, nil
}

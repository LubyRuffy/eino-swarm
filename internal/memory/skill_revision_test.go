package memory

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRestoreSkillRevisionUsesExactToolReceiptAndPreservesMetadata(t *testing.T) {
	s, tools, changes := toolset(t, 2000)
	const name = "verified-procedure"
	const description = "when checking the original procedure: confirm the source"
	const originalBody = "Preserve the verified preconditions.\nThen retain the original output."
	const revisedLine = "Apply the unsupported preconditions."
	if _, err := s.ImportSkill(name, description, originalBody, SkillOrigin{ProjectID: "source-project", Name: "source-procedure"}); err != nil {
		t.Fatal(err)
	}
	path := s.skillPath(name)
	// Historical skills may use equivalent front-matter quoting. Restoring the
	// complete receipt must keep those bytes instead of rendering a new file.
	original := strings.Replace(readSkillRevisionFile(t, path), "name: "+name, `name: "`+name+`"`, 1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	initial, err := s.ViewSkill(name)
	if err != nil {
		t.Fatal(err)
	}
	usage := readSkillRevisionFile(t, s.skillUsagePath(name))
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "patch", "name": name, "old_text": "Preserve the verified preconditions.", "new_text": revisedLine}); out["success"] != true {
		t.Fatalf("prepare actual patch: %v", out)
	}
	if len(*changes) != 1 || (*changes)[0].After == nil || (*changes)[0].Before == nil || *(*changes)[0].Before != original {
		t.Fatal("patch did not report the complete original file")
	}
	patch := (*changes)[0]
	got, raw, err := s.ReadSkillRevision(name)
	if err != nil || raw != *patch.After || got.Description != description || !strings.Contains(got.Body, revisedLine) || got.Origin == nil || *got.Origin != *initial.Origin || got.Views != initial.Views {
		t.Fatalf("revision read did not bind the actual parsed skill and exact document: %+v (%v)", got, err)
	}
	change, changed, err := s.RestoreSkillRevision(name, *patch.After, *patch.Before)
	if err != nil || !changed || change.Target != ToolSkillManage || change.Action != "patch" || change.Name != name {
		t.Fatalf("restore: change=%+v changed=%v err=%v", change, changed, err)
	}
	assertChangeContent(t, change, *patch.After, original)
	got, raw, err = s.ReadSkillRevision(name)
	if err != nil || raw != original || got.Body != originalBody || got.Description != description || got.Origin == nil ||
		*got.Origin != *initial.Origin || got.Views != initial.Views || !got.LastViewedAt.Equal(initial.LastViewedAt) ||
		readSkillRevisionFile(t, s.skillUsagePath(name)) != usage {
		t.Fatalf("restore changed metadata, usage, or exact file bytes: %+v (%v)", got, err)
	}
	oldTime := time.Unix(1234, 0)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{*patch.After, original} {
		if again, changed, err := s.RestoreSkillRevision(name, expected, original); err != nil || changed || again != (Change{}) {
			t.Fatalf("same state wrote a receipt: %+v (%v, %v)", again, changed, err)
		}
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(oldTime) {
		t.Fatalf("same state wrote the file: info=%v err=%v", info, err)
	}
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "patch", "name": name, "old_text": originalBody, "new_text": "Apply a later supported revision."}); out["success"] != true {
		t.Fatalf("automatic restore blocked a later valid edit: %v", out)
	}
}

func TestRestoreSkillRevisionKeepsLaterMetadataOnlyEdit(t *testing.T) {
	s, tools, changes := toolset(t, 2000)
	const name = "verified-procedure"
	const originalBody = "Preserve the verified preconditions."
	const revisedBody = "Apply the unsupported preconditions."
	if _, err := s.WriteSkill(name, "when checking the original procedure", originalBody); err != nil {
		t.Fatal(err)
	}
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "patch", "name": name, "old_text": originalBody, "new_text": revisedBody}); out["success"] != true {
		t.Fatalf("prepare actual patch: %v", out)
	}
	if len(*changes) != 1 || (*changes)[0].After == nil || (*changes)[0].Before == nil {
		t.Fatal("patch did not report exact documents")
	}
	if _, err := s.WriteSkill(name, "a later editor narrowed the scope", revisedBody); err != nil {
		t.Fatal(err)
	}
	path := s.skillPath(name)
	current := readSkillRevisionFile(t, path)
	change, changed, err := s.RestoreSkillRevision(name, *(*changes)[0].After, *(*changes)[0].Before)
	if !errors.Is(err, ErrSkillRevisionConflict) || changed || change != (Change{}) || readSkillRevisionFile(t, path) != current {
		t.Fatalf("stale rollback did not preserve the exact later edit: changed=%v, err=%v", changed, err)
	}
}

func TestRestoreSkillRevisionRequiresAnExistingCompleteSameSkill(t *testing.T) {
	const name = "verified-procedure"
	const description = "when checking a recorded procedure"
	expected := renderSkill(name, description, "Expected content.", nil)
	previous := renderSkill(name, description, "Previous content.", nil)
	s := New(t.TempDir(), 2000)
	plantSkill(t, s.dir, name, description, "Expected content.")
	for _, tc := range []struct {
		label, target, from, to string
		want                    error
	}{
		{"invalid name", "../escape", expected, previous, ErrBadName},
		{"missing target", "absent-skill", renderSkill("absent-skill", description, "Expected content.", nil), renderSkill("absent-skill", description, "Previous content.", nil), ErrSkillRevisionConflict},
		{"no current document", name, "Expected content.", previous, nil},
		{"no previous document", name, expected, "Previous content.", nil},
		{"previous has different name", name, expected, renderSkill("different-skill", description, "Previous content.", nil), nil},
		{"previous has different description", name, expected, renderSkill(name, "a different scope", "Previous content.", nil), nil},
		{"previous has no body", name, expected, renderSkill(name, description, "", nil), nil},
		{"expected has no body", name, renderSkill(name, description, "", nil), previous, nil},
	} {
		t.Run(tc.label, func(t *testing.T) {
			change, changed, err := s.RestoreSkillRevision(tc.target, tc.from, tc.to)
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) || changed || change != (Change{}) || readSkillRevisionFile(t, s.skillPath(name)) != expected {
				t.Fatalf("ambiguous restore was not refused: %+v (%v, %v)", change, changed, err)
			}
		})
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if change, changed, err := draft.Store.RestoreSkillRevision(name, expected, previous); err == nil || changed || change != (Change{}) || readSkillRevisionFile(t, s.skillPath(name)) != expected {
		t.Fatalf("notes draft changed a live skill: %+v (%v, %v)", change, changed, err)
	}
	if _, _, err := s.ReadSkillRevision("../escape"); !errors.Is(err, ErrBadName) {
		t.Fatalf("invalid read name: %v", err)
	}
	if _, _, err := s.ReadSkillRevision("absent-skill"); !errors.Is(err, ErrSkillRevisionConflict) {
		t.Fatalf("missing read target: %v", err)
	}
}

func TestSkillRevisionRefusesAmbiguousPathsWithoutChangingFiles(t *testing.T) {
	const name = "verified-procedure"
	const description = "when checking a recorded procedure"
	expected := renderSkill(name, description, "Expected content.", nil)
	previous := renderSkill(name, description, "Previous content.", nil)
	for _, target := range []string{"root", "skills", "skill", "document", "temporary symlink", "temporary file", "document directory"} {
		t.Run(target, func(t *testing.T) {
			s := New(filepath.Join(t.TempDir(), "memory"), 2000)
			plantSkill(t, s.dir, name, description, "Expected content.")
			path := s.skillPath(name)
			alias := map[string]string{
				"root": s.dir, "skills": s.skillsDir(), "skill": filepath.Dir(path), "document": path,
				"temporary symlink": path + ".tmp", "temporary file": path + ".tmp", "document directory": path,
			}[target]
			if target == "temporary file" {
				if err := os.WriteFile(alias, []byte("separate contents"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if target == "document directory" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				moved := filepath.Join(t.TempDir(), "moved")
				if target != "temporary symlink" {
					if err := os.Rename(alias, moved); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(moved, alias); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("filesystem does not grant symbolic link creation: %v", err)
					}
					t.Fatal(err)
				}
				if target == "root" {
					s.dir += string(os.PathSeparator) + "."
				}
			}
			change, changed, err := s.RestoreSkillRevision(name, expected, previous)
			if !errors.Is(err, errUnsafeSkillRevisionPath) || changed || change != (Change{}) {
				t.Fatalf("ambiguous path accepted: %+v (%v, %v)", change, changed, err)
			}
			if target == "temporary symlink" || target == "temporary file" {
				if _, got, err := s.ReadSkillRevision(name); err != nil || got != expected {
					t.Fatalf("restore mutated a valid current file: %v", err)
				}
				if target == "temporary file" && readSkillRevisionFile(t, alias) != "separate contents" {
					t.Fatal("restore overwrote a preexisting temporary file")
				}
				return
			}
			if _, _, err := s.ReadSkillRevision(name); !errors.Is(err, errUnsafeSkillRevisionPath) {
				t.Fatalf("revision read followed an ambiguous path: %v", err)
			}
			if target != "document directory" && readSkillRevisionFile(t, path) != expected {
				t.Fatal("restore mutated the referenced file")
			}
		})
	}
}

func TestSkillRevisionStorageFailureReportsNoChange(t *testing.T) {
	const name = "verified-procedure"
	const description = "when checking a recorded procedure"
	expected := renderSkill(name, description, "Expected content.", nil)
	previous := renderSkill(name, description, "Previous content.", nil)
	for _, failure := range []string{"read", "write"} {
		t.Run(failure, func(t *testing.T) {
			s := New(t.TempDir(), 2000)
			plantSkill(t, s.dir, name, description, "Expected content.")
			path := s.skillPath(name)
			guarded, mode, restored := path, os.FileMode(0), os.FileMode(0o600)
			if failure == "write" {
				guarded, mode, restored = filepath.Dir(path), 0o500, 0o700
			}
			if err := os.Chmod(guarded, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(guarded, restored); err != nil {
					t.Error(err)
				}
			})
			var probeErr error
			if failure == "read" {
				_, probeErr = os.ReadFile(path)
			} else {
				probeErr = os.WriteFile(path+".test-probe", []byte("probe"), 0o600)
			}
			if probeErr == nil {
				t.Skip("filesystem grants the tested process access despite the permission mode")
			}
			if !errors.Is(probeErr, os.ErrPermission) {
				t.Fatalf("unexpected storage failure from capability probe: %v", probeErr)
			}
			change, changed, err := s.RestoreSkillRevision(name, expected, previous)
			if !errors.Is(err, os.ErrPermission) || changed || change != (Change{}) {
				t.Fatalf("storage failure reported an applied rollback: %+v (%v, %v)", change, changed, err)
			}
			if err := os.Chmod(guarded, restored); err != nil {
				t.Fatal(err)
			}
			if readSkillRevisionFile(t, path) != expected {
				t.Fatal("failed restore changed the current file")
			}
		})
	}
}

func TestRestoreSkillRevisionAndManualWriteShareStoreLock(t *testing.T) {
	const name = "verified-procedure"
	const description = "when checking a recorded procedure"
	const editorBody = "A later human replacement."
	expected := renderSkill(name, description, "Expected content.", nil)
	previous := renderSkill(name, description, "Previous content.", nil)
	s := New(t.TempDir(), 2000)
	plantSkill(t, s.dir, name, description, "Expected content.")
	start := make(chan struct{})
	rollback, edit := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		_, _, err := s.RestoreSkillRevision(name, expected, previous)
		rollback <- err
	}()
	go func() {
		<-start
		_, err := s.WriteSkill(name, description, editorBody)
		edit <- err
	}()
	close(start)
	if err := <-rollback; err != nil && !errors.Is(err, ErrSkillRevisionConflict) {
		t.Fatalf("unexpected rollback error: %v", err)
	}
	if err := <-edit; err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.ReadSkillRevision(name); err != nil || got.Body != editorBody {
		t.Fatalf("stale rollback erased a concurrent human edit: %+v (%v)", got, err)
	}
}

func readSkillRevisionFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

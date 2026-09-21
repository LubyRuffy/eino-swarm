package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The 4096-byte encoding probe used to cut a UTF-8 rune in half and then
// relabel the whole file GB18030. A Chinese notes file became mojibake, and
// the model trusted the header. This is the pin canary for that eino-tools fix.
func TestReadKeepsUTF8WhenTheProbeWindowCutsARune(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	const marker = "结构相似度"
	raw := append(bytes.Repeat([]byte("x"), 4095), []byte(marker+"\n")...)
	if !utf8.Valid(raw) {
		t.Fatal("fixture must be valid UTF-8")
	}
	if utf8.Valid(raw[:4096]) {
		t.Fatal("the 4096-byte probe must be invalid so this test actually hits the old bug")
	}
	path := filepath.Join(ws, "notes.md")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := find(t, set, "read").InvokableRun(context.Background(), `{"file_path":"notes.md"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "encoding=utf-8") {
		t.Fatalf("want utf-8 probe, got %q", out[:min(120, len(out))])
	}
	if strings.Contains(out, "encoding=gb18030") {
		t.Fatalf("truncated UTF-8 was labelled gb18030: %q", out[:min(120, len(out))])
	}
	if !strings.Contains(out, marker) {
		t.Fatalf("body lost the UTF-8 marker: %q", out[:min(200, len(out))])
	}
}

// Empty replace_block is a delete. The old AND of two non-empty strings
// treated "" as "no payload" and the model retried forever.
func TestEditEmptyReplaceDeletesTheMatchedBlock(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	path := filepath.Join(ws, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{
		"file_path":     "notes.txt",
		"search_block":  "beta\n",
		"replace_block": "",
	})
	out, err := find(t, set, "edit").InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if strings.HasPrefix(out, "error:") {
		t.Fatalf("empty replace must delete, got %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\ngamma\n" {
		t.Fatalf("after delete: %q", got)
	}
}

func TestEditReplaceWithoutSearchNamesTheMissingField(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "notes.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := find(t, set, "edit").InvokableRun(context.Background(),
		`{"file_path":"notes.txt","replace_block":"nope\n"}`)
	if err != nil {
		t.Fatalf("edit must not return a Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want a tool error, got %q", out)
	}
	if !strings.Contains(out, "search_block is required") {
		t.Fatalf("error must name the missing field, got %q", out)
	}
	if strings.Contains(out, "either search_block/replace_block or patch is required") {
		t.Fatalf("old catch-all error came back: %q", out)
	}
}

// Omitting content used to WriteFile("") and still return Updated file.
// The model trusted the status line while the file on disk was emptied.
func TestWriteOmittedContentDoesNotEmptyTheFile(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	path := filepath.Join(ws, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := find(t, set, "write").InvokableRun(context.Background(),
		`{"file_path":"notes.txt"}`)
	if err != nil {
		t.Fatalf("write must not return a Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want a tool error, got %q", out)
	}
	if !strings.Contains(out, "content is required") {
		t.Fatalf("error must name the missing field, got %q", out)
	}
	if strings.Contains(out, "Updated file") {
		t.Fatalf("omitted content must not claim success: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\n" {
		t.Fatalf("file was mutated: %q", got)
	}
}

// Models trained on a contents field used to empty the file and hear success.
func TestWriteContentsKeyIsRejected(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	path := filepath.Join(ws, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{
		"file_path": "notes.txt",
		"contents":  "beta\n",
	})
	out, err := find(t, set, "write").InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("write must not return a Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want a tool error, got %q", out)
	}
	if !strings.Contains(out, "use content not contents") {
		t.Fatalf("error must name the wrong key, got %q", out)
	}
	if strings.Contains(out, "Updated file") {
		t.Fatalf("wrong key must not claim success: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\n" {
		t.Fatalf("file was mutated: %q", got)
	}
}

// Success is bytes readable at the resolved path, not the caller's relative
// string. exec that cd's into a subdirectory and cats the same relative path
// is looking at a different file; the status line has to say which one landed.
func TestWriteSuccessNamesResolvedPathAndBytes(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	body := "line 1\nline \"2\"\n"
	args, _ := json.Marshal(map[string]string{
		"file_path": "notes.txt",
		"content":   body,
	})
	out, err := find(t, set, "write").InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if strings.HasPrefix(out, "error:") {
		t.Fatalf("want success, got %q", out)
	}
	abs := filepath.Join(ws, "notes.txt")
	if !strings.Contains(out, abs) {
		t.Fatalf("success must name the resolved path, got %q", out)
	}
	if !strings.Contains(out, "bytes") {
		t.Fatalf("success must name the byte count, got %q", out)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("disk=%q", got)
	}
}

func TestWriteRelativePathLandsInWorkspaceNotCwd(t *testing.T) {
	cfg := configFor(t)
	ws := filepath.Join(t.TempDir(), "ws")
	cwd := t.TempDir()
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Error(err)
		}
	})

	args, _ := json.Marshal(map[string]string{
		"file_path": "notes.txt",
		"content":   "beta\n",
	})
	out, err := find(t, set, "write").InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if strings.HasPrefix(out, "error:") {
		t.Fatalf("want success, got %q", out)
	}
	abs := filepath.Join(ws, "notes.txt")
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("file did not land in the workspace: %v", err)
	}
	if string(got) != "beta\n" {
		t.Fatalf("workspace content=%q", got)
	}
	if _, err := os.Stat(filepath.Join(cwd, "notes.txt")); err == nil {
		t.Fatal("relative write must not follow process cwd")
	}
	if !strings.Contains(out, abs) {
		t.Fatalf("success named the wrong path: %q", out)
	}
}

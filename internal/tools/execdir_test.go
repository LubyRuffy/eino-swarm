package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execArgs(t *testing.T, fields map[string]string) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A model that guessed a working directory got "fork/exec <shell>: no such
// file or directory" from the real exec tool, concluded the shell was
// missing, and abandoned exec for the rest of the conversation. The error
// must name the directory, through the tool the agents actually call.
func TestExecNamesAMissingWorkingDirectoryInsteadOfTheShell(t *testing.T) {
	cfg := configFor(t)
	ws := t.TempDir()
	set, err := Build(context.Background(), cfg, ws)
	if err != nil {
		t.Fatal(err)
	}
	execTool := find(t, set, "exec")
	missing := filepath.Join(t.TempDir(), "not-created")

	for _, cwd := range []string{missing, "not-created-either"} {
		_, err = execTool.InvokableRun(context.Background(), execArgs(t, map[string]string{"command": "echo ran", "cwd": cwd}))
		if err == nil {
			t.Fatalf("cwd %q: a missing directory ran the command", cwd)
		}
		if !strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "fork/exec") {
			t.Fatalf("cwd %q: error does not name the directory: %v", cwd, err)
		}
	}

	if err := os.Mkdir(filepath.Join(ws, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := execTool.InvokableRun(context.Background(), execArgs(t, map[string]string{"command": "pwd", "cwd": "sub"}))
	if err != nil || !strings.Contains(out, "sub") {
		t.Fatalf("an existing relative cwd must still run there: out=%s err=%v", out, err)
	}
}

func TestMissingExecWorkDirResolvesLikeExec(t *testing.T) {
	ws := t.TempDir()
	other := t.TempDir()
	file := filepath.Join(ws, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(other, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, args string
		wantErr    string
	}{
		{"no cwd", `{"command":"true"}`, ""},
		{"malformed arguments are exec's to report", `{`, ""},
		{"relative under base_dir", `{"command":"true","cwd":"inside","base_dir":"` + other + `"}`, ""},
		{"relative missing under workspace", `{"command":"true","cwd":"inside"}`, "does not exist"},
		{"a file is not a directory", `{"command":"true","cwd":"` + file + `"}`, "not a directory"},
	}
	for _, tc := range cases {
		err := missingExecWorkDir(tc.args, ws)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Fatalf("%s: unexpected refusal %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Fatalf("%s: err=%v want %q", tc.name, err, tc.wantErr)
		}
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(t.TempDir(), "locked")
		if err := os.Mkdir(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		err := missingExecWorkDir(`{"command":"true","cwd":"`+filepath.Join(locked, "inner")+`"}`, ws)
		if err == nil || !strings.Contains(err.Error(), "not accessible") {
			t.Fatalf("an unreadable parent must be named as inaccessible, not missing: %v", err)
		}
	}
	if err := missingExecWorkDir(`{"command":"true","cwd":"rel"}`, ""); err != nil {
		t.Fatalf("no base to resolve against is exec's to handle: %v", err)
	}
	gate := wrapExecWorkDir(&stubExec{}, ws)
	if info, err := gate.Info(context.Background()); err != nil || info.Name != "exec" {
		t.Fatalf("Info must pass through: %+v %v", info, err)
	}
}

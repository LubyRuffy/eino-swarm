//go:build unix

package frontend

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This fixture uses the real offline npm -> shell -> node chain. Cancelling
// npm alone leaves its Node child writing into the next build's dist tree.
func TestCancelledNpmBuildStopsDescendantsBeforeReleasingTheBuildLock(t *testing.T) {
	for _, tool := range []string{"npm", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("offline process-tree fixture requires %s: %v", tool, err)
		}
	}
	root := writeTree(t)
	mustWrite(t, filepath.Join(root, "package.json"), `{"name":"offline-build-fixture","scripts":{"build":"node fixture.cjs"}}`)
	mustWrite(t, filepath.Join(root, "fixture.cjs"), `const fs = require("fs");
fs.mkdirSync(".build-fixture", {recursive: true});
fs.mkdirSync("dist", {recursive: true});
fs.writeFileSync(".build-fixture/child.pid", String(process.pid));
setInterval(() => fs.appendFileSync("dist/writes", "x"), 25);
`)
	// This tests command ownership; no registry or package download is used.
	newer := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(root, "node_modules"), newer, newer); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ensureBundle(ctx, root, execRunner{}) }()
	pidPath := filepath.Join(root, ".build-fixture", "child.pid")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if body, err := os.ReadFile(pidPath); err == nil {
			pid, _ := strconv.Atoi(string(body))
			t.Cleanup(func() {
				if pid > 0 && pid != os.Getpid() {
					process, _ := os.FindProcess(pid)
					_ = process.Kill()
				}
			})
			break
		}
		select {
		case err := <-done:
			t.Fatalf("offline npm build exited before its child started: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("offline npm build did not start its Node descendant")
		}
		time.Sleep(time.Millisecond)
	}
	writes := filepath.Join(root, "dist", "writes")
	for {
		if info, err := os.Stat(writes); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Node descendant did not write to the build output")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled npm lost its cancellation category: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled npm did not finish terminating its process tree")
	}
	before, err := os.ReadFile(writes)
	if err != nil {
		t.Fatal(err)
	}
	// A subsequent caller must acquire the lock only once the old writer has
	// stopped, then complete its own bundle rather than sharing the output.
	if err := ensureBundle(t.Context(), root, &scriptRunner{writeIndex: true}); err != nil {
		t.Fatalf("cancelled build did not release its lock for the next build: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(writes)
	if err != nil || len(after) != len(before) {
		t.Fatalf("cancelled build's descendant wrote after the lock was reused: before=%d after=%d err=%v", len(before), len(after), err)
	}
}

func TestNpmExitStopsBackgroundDescendantsBeforeReturning(t *testing.T) {
	previousOutput := ensureOut
	ensureOut = io.Discard
	t.Cleanup(func() { ensureOut = previousOutput })
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Skipf("offline process-tree fixture requires npm: %v", err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("offline process-tree fixture requires node: %v", err)
	}
	for _, code := range []string{"0", "7"} {
		t.Run(code, func(t *testing.T) {
			root := t.TempDir()
			t.Cleanup(func() {
				body, _ := os.ReadFile(filepath.Join(root, "child.pid"))
				pid, _ := strconv.Atoi(string(body))
				if pid > 0 && pid != os.Getpid() {
					process, _ := os.FindProcess(pid)
					_ = process.Kill()
				}
			})
			mustWrite(t, filepath.Join(root, "package.json"), `{"name":"offline-build-fixture","scripts":{"build":"node fixture.cjs"}}`)
			mustWrite(t, filepath.Join(root, "writer.cjs"), `const fs = require("fs");
fs.mkdirSync("dist", {recursive: true});
fs.writeFileSync("child.pid", String(process.pid));
setInterval(() => fs.appendFileSync("dist/writes", "x"), 25);
`)
			mustWrite(t, filepath.Join(root, "fixture.cjs"), `const fs = require("fs");
require("child_process").spawn(process.execPath, ["writer.cjs"], {stdio: "inherit"}).unref();
setInterval(() => { if (fs.existsSync("dist/writes")) process.exit(`+code+`); }, 5);
`)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			err := (execRunner{}).Run(ctx, root, npm, npmRunArg, npmBuildScript)
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("finished npm was blocked by a descendant holding stdout: %v", err)
			}
			if (code == "0") != (err == nil) {
				t.Fatalf("npm exit category changed: code=%s err=%v", code, err)
			}
			path := filepath.Join(root, "dist", "writes")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			after, err := os.ReadFile(path)
			if err != nil || len(before) != len(after) {
				t.Fatalf("npm exited but its background writer survived: before=%d after=%d err=%v", len(before), len(after), err)
			}
		})
	}
}

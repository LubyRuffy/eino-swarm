package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
)

func TestBindExecOutputForwardsStdoutBeforeWait(t *testing.T) {
	tl, err := einoexec.New(einoexec.Config{})
	if err != nil {
		t.Fatal(err)
	}

	first := make(chan string, 1)
	var mu sync.Mutex
	var latest string
	ctx := BindExecOutput(context.Background(), func(text string) {
		mu.Lock()
		latest = text
		mu.Unlock()
		select {
		case first <- text:
		default:
		}
	}, einoexec.ToolName, "c1")

	done := make(chan struct{})
	var result string
	go func() {
		defer close(done)
		result, _ = tl.InvokableRun(ctx, `{"command":"printf a; sleep 0.3; printf b","timeout_ms":5000}`)
	}()

	select {
	case got := <-first:
		var payload map[string]string
		if err := json.Unmarshal([]byte(got), &payload); err != nil {
			t.Fatalf("live payload not JSON: %v (%s)", err, got)
		}
		if payload["stdout"] != "a" {
			t.Fatalf("first live stdout = %q", payload["stdout"])
		}
	case <-done:
		t.Fatal("exec finished before a live chunk — BindExecOutput is not wired")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live exec output")
	}

	<-done
	if !strings.Contains(result, `"stdout":"ab"`) && !strings.Contains(result, `"stdout": "ab"`) {
		t.Fatalf("final result missing both chunks: %s", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(latest, `"stdout":"ab"`) && !strings.Contains(latest, "ab") {
		t.Fatalf("accumulated live text: %s", latest)
	}
}

func TestBindExecOutputIgnoresOtherTools(t *testing.T) {
	for _, name := range []string{"read", "python_runner"} {
		called := false
		ctx := BindExecOutput(context.Background(), func(string) { called = true }, name, "c1")
		if ctx != context.Background() {
			t.Fatalf("%s must not wrap the context", name)
		}
		if called {
			t.Fatalf("listener must not fire for %s", name)
		}
	}
}

func TestBindExecOutputIgnoresANilEmitter(t *testing.T) {
	ctx := BindExecOutput(context.Background(), nil, einoexec.ToolName, "c1")
	if ctx != context.Background() {
		t.Fatal("a nil emitter must not wrap the context")
	}
}

func TestBindExecOutputForwardsStderr(t *testing.T) {
	tl, err := einoexec.New(einoexec.Config{})
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	ctx := BindExecOutput(context.Background(), func(text string) {
		select {
		case got <- text:
		default:
		}
	}, einoexec.ToolName, "c1")
	if _, err := tl.InvokableRun(ctx, `{"command":"printf err >&2","timeout_ms":5000}`); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-got:
		var payload map[string]string
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("live payload not JSON: %v (%s)", err, text)
		}
		if payload["stderr"] != "err" {
			t.Fatalf("live stderr = %q", payload["stderr"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live stderr")
	}
}

func TestLiveExecJSONClipsEachStream(t *testing.T) {
	long := strings.Repeat("x", liveExecNotifyLimit+8)
	raw := liveExecJSON(long, "tail")
	var payload map[string]string
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if got := []rune(payload["stdout"]); len(got) != liveExecNotifyLimit+1 || !strings.HasSuffix(payload["stdout"], "…") {
		t.Fatalf("stdout runes=%d suffix=%q", len(got), payload["stdout"][len(payload["stdout"])-1:])
	}
	if payload["stderr"] != "tail" {
		t.Fatalf("stderr = %q", payload["stderr"])
	}
}

func TestClipRunes(t *testing.T) {
	if got := clipRunes("ab", 0); got != "ab" {
		t.Fatalf("n=0 should leave the string, got %q", got)
	}
	if got := clipRunes("abcd", 3); got != "abc…" {
		t.Fatalf("clip = %q", got)
	}
	if got := clipRunes("目目目", 2); got != "目目…" {
		t.Fatalf("rune clip = %q", got)
	}
}

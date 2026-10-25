//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// The same live engine can become unreachable at different handoff boundaries.
// Its diagnostic must identify that boundary without changing ownership or cause.
func TestRecoveryEngineVerificationReportsItsActualHandoffPhase(t *testing.T) {
	for _, phase := range []string{"parent prelaunch", "successor attach", "post-ready precommit"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			var metadataRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/meta" {
					t.Errorf("unexpected recovery request: %s", r.URL.Path)
				}
				metadataRequests.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, "private-response-body")
			}))
			defer server.Close()
			record := lease.Record{PID: os.Getpid(), URL: server.URL, Mock: true, Version: "fixture", Dev: 1, Ino: 2}
			if err := lease.Publish(dir, record); err != nil {
				t.Fatal(err)
			}
			spec := desktopRecoverySpec{SessionID: "phase", Token: "private-handoff-token", DataDir: dir,
				Mock: true, Engine: record, Generation: 1, Attempts: 1, Journal: filepath.Join(dir, "recovery.jsonl")}
			var err error
			switch phase {
			case "parent prelaunch":
				launched := false
				opts, cleanup, attachErr := attachDesktopRecoveryWithLauncher(
					desktop.Options{URL: server.URL, Fatal: make(chan error)}, spec, nil,
					func(context.Context, desktopRecoverySpec, *exec.Cmd) error {
						launched = true
						return nil
					})
				if attachErr != nil {
					t.Fatal(attachErr)
				}
				defer cleanup()
				err = opts.Recovery.Relaunch(context.Background(), desktop.RecoveryAttempt{Generation: 2, Attempts: 1})
				if launched {
					t.Fatal("unverified original engine launched a successor")
				}
			case "successor attach":
				var input, ready bytes.Buffer
				if err := json.NewEncoder(&input).Encode(spec); err != nil {
					t.Fatal(err)
				}
				previous := openDesktopWindow
				defer func() { openDesktopWindow = previous }()
				opened := false
				openDesktopWindow = func(desktop.Options) error {
					opened = true
					return nil
				}
				err = runDesktopAttachment(&input, &ready)
				if opened || ready.Len() != 0 {
					t.Fatal("unverified successor opened a window or claimed renderer readiness")
				}
			case "post-ready precommit":
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
				command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD=ready")
				err = runRecoverySuccessor(ctx, spec, command)
				if command.ProcessState == nil {
					t.Fatal("post-ready refusal did not reap its uncommitted successor")
				}
			}
			if !errors.Is(err, lease.ErrUnreachable) {
				t.Fatalf("recovery phase lost the original unreachable cause: %v", err)
			}
			prefix := "desktop recovery " + phase + ": "
			if !strings.HasPrefix(err.Error(), prefix) {
				t.Fatalf("recovery failure does not identify %s: %v", phase, err)
			}
			for _, secret := range []string{spec.Token, record.URL, "private-response-body"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("recovery diagnostic exposed private handoff or response data")
				}
			}
			if metadataRequests.Load() != 1 {
				t.Fatalf("verification changed the request/retry count: %d", metadataRequests.Load())
			}
		})
	}
}

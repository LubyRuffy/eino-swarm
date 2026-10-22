package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeNormalAndEarlyExitDoNotReportTerminalFailure(t *testing.T) {
	for _, mode := range []string{"nil channel", "closed channel", "nil notice", "normal release", "early fatal"} {
		t.Run(mode, func(t *testing.T) {
			var fatal chan error
			if mode != "nil channel" {
				fatal = make(chan error, 1)
			}
			var want error
			switch mode {
			case "closed channel":
				close(fatal)
			case "nil notice":
				fatal <- nil
			case "early fatal":
				want = fmt.Errorf("connection ended before startup: %w", io.EOF)
				fatal <- want
			}
			var logged bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logged, nil))
			err := runWithFatal(func() error {
				if mode == "early fatal" {
					t.Fatal("early failure opened the native app")
				}
				if mode == "normal release" {
					close(fatal)
				}
				return nil
			}, nativeFatalQuit(log, func() { t.Error("normal or early exit requested native Quit") }), make(chan struct{}), fatal)
			if !errors.Is(err, want) {
				t.Fatalf("native return cause changed: got %v, want %v", err, want)
			}
			if logged.Len() != 0 {
				t.Fatalf("normal or early exit logged a native terminal failure: %s", logged.String())
			}
		})
	}
}

// AppKit termination may end the process without returning from Run. The
// connection failure must already be observable when native Quit starts.
func TestNativeFailureIsReportedBeforeNonReturningQuit(t *testing.T) {
	const child = "ZWAI_TEST_NATIVE_TERMINAL_EXIT"
	const cause = "desktop: native presence recovery exhausted"
	const quitting = "native-quit-entered"
	if os.Getenv(child) == "1" {
		started := make(chan struct{})
		close(started)
		fatal := make(chan error)
		log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "native-test")
		runWithFatal(func() error {
			fatal <- fmt.Errorf("%s: %w", cause, io.EOF)
			select {}
		}, nativeFatalQuit(log, func() {
			fmt.Fprintln(os.Stderr, quitting)
			os.Exit(23)
		}), started, fatal)
		os.Exit(24)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestNativeFailureIsReportedBeforeNonReturningQuit$")
	cmd.Env = append(os.Environ(), child+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("native termination child did not exit: %v; output %s", ctx.Err(), out)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 {
		t.Fatalf("native Quit did not terminate the child: %v; output %s", err, out)
	}
	text := string(out)
	if strings.Count(text, cause) != 1 {
		t.Fatalf("terminal cause must be reported once before native termination; output %s", out)
	}
	if strings.Index(text, cause) > strings.Index(text, quitting) {
		t.Fatalf("native Quit started before the cause was reported; output %s", out)
	}
	if !strings.Contains(text, "component=native-test") || !strings.Contains(text, "level=ERROR") {
		t.Fatalf("terminal cause did not use the configured structured logger: %s", out)
	}
}

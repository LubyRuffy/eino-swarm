//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

func TestRecoveryResourceFailureDoesNotLaunchOrClaimDurability(t *testing.T) {
	if mode := os.Getenv("ZWAI_TEST_RECOVERY_RESOURCE"); mode != "" {
		if mode == "journal-full" {
			testRecoveryJournalFileLimit(t)
		} else if mode == "journal-fifo" {
			testRecoveryJournalUnsupportedLock(t)
		} else {
			testRecoveryPipeFileLimit(t, mode == "second-pipe")
		}
		return
	}
	for _, mode := range []string{"first-pipe", "second-pipe", "journal-full", "journal-fifo"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryResourceFailureDoesNotLaunchOrClaimDurability$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_RESOURCE="+mode)
			// Test binaries choose their counter directory with this flag rather
			// than the inherited GOCOVERDIR used by ordinary instrumented binaries.
			if coverDir := os.Getenv("GOCOVERDIR"); coverDir != "" {
				command.Args = append(command.Args, "-test.gocoverdir="+coverDir)
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("isolated resource failure %s: %v\n%s", mode, err, output)
			}
		})
	}
}

func testRecoveryPipeFileLimit(t *testing.T, second bool) {
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	if limited.Cur > 64 {
		limited.Cur = 64
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
		t.Fatal(err)
	}
	var files []*os.File
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
			t.Error(err)
		}
	}()
	for {
		file, err := os.Open(os.DevNull)
		if errors.Is(err, syscall.EMFILE) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	available := 0
	if second {
		if len(files) < 2 {
			t.Fatal("test child could not reserve the first pipe's descriptors")
		}
		for i := len(files) - 2; i < len(files); i++ {
			_ = files[i].Close()
		}
		files = files[:len(files)-2]
		available = 2
	}
	command := exec.Command("/usr/bin/false")
	if err := runRecoverySuccessor(context.Background(), desktopRecoverySpec{}, command); !errors.Is(err, syscall.EMFILE) {
		t.Fatalf("descriptor exhaustion changed its category: %v", err)
	}
	if command.Process != nil {
		t.Fatal("successor started without both private handoff pipes")
	}
	for i := 0; i < available; i++ {
		file, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatalf("failed pipe setup leaked a descriptor: %v", err)
		}
		files = append(files, file)
	}
	file, err := os.Open(os.DevNull)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, syscall.EMFILE) {
		t.Fatalf("failed launch changed its descriptor ownership: %v", err)
	}
}

func testRecoveryJournalFileLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.jsonl")
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	limited.Cur = 0
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Error(err)
		}
	}()
	err := appendRecoveryDiagnostic(path, desktop.RecoveryDiagnostic{SessionID: "resource", Phase: "ready", At: time.Now()})
	if !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("journal write failure claimed durability or lost its category: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("failed journal write left a partial receipt: %v %v", info, err)
	}
}

func TestRecoveryJournalRejectsANonRegularOutputWithoutClaimingDurability(t *testing.T) {
	if err := appendRecoveryDiagnostic(os.DevNull, desktop.RecoveryDiagnostic{SessionID: "resource", Phase: "ready", At: time.Now()}); err == nil {
		t.Fatal("unsupported journal destination claimed a durable receipt")
	}
}

func testRecoveryJournalUnsupportedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	err := appendRecoveryDiagnostic(path, desktop.RecoveryDiagnostic{SessionID: "resource", Phase: "ready", At: time.Now()})
	if !errors.Is(err, syscall.ENOTSUP) && !errors.Is(err, syscall.EOPNOTSUPP) {
		t.Fatalf("unsupported journal lock claimed durability or lost its category: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("failed journal locking replaced its destination: %v %v", info, err)
	}
}

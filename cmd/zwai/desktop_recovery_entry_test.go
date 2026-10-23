//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

func TestRecoveryCopyCleanupFailureRetainsItsOwnerAndLeavesOtherCopiesAlone(t *testing.T) {
	parent := t.TempDir()
	private := filepath.Join(parent, "desktop-recovery-owned")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(private, "session")
	if err := os.WriteFile(marker, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(parent, "desktop-recovery-other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "session"), []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(private, 0700)
	removeDesktopRecoveryCopy(desktopRecoverySpec{PrivateDir: private, SessionID: "owned"})
	if body, err := os.ReadFile(marker); err != nil || string(body) != "owned" {
		t.Fatalf("failed cleanup lost its owner: %q %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(other, "session")); err != nil || string(body) != "other" {
		t.Fatalf("cleanup affected another copy: %q %v", body, err)
	}
}

func TestRecoverySystemPathFailureStopsBeforePreparingOrAttaching(t *testing.T) {
	failure := errors.New("system path unavailable")
	for _, mode := range []string{"cache", "executable", "invalid-argument"} {
		t.Run(mode, func(t *testing.T) {
			cacheCalls, executableCalls := 0, 0
			opts := desktop.Options{URL: "http://127.0.0.1:1", Title: "original"}
			cache := func() (string, error) {
				cacheCalls++
				if mode == "cache" {
					return "", failure
				}
				return t.TempDir(), nil
			}
			executable := func() (string, error) {
				executableCalls++
				if mode == "executable" {
					return "", failure
				}
				return "unused", nil
			}
			configured, cleanup, err := configureDesktopRecoveryWithSystemPaths(opts, []string{"--unknown"}, cache, executable)
			if err == nil || cleanup != nil || configured.Recovery != nil || configured.Title != opts.Title || configured.URL != opts.URL {
				t.Fatalf("failed setup changed shell ownership: %+v %v", configured, err)
			}
			if mode != "invalid-argument" && !errors.Is(err, failure) {
				t.Fatalf("system path error lost its category: %v", err)
			}
			wantExecutableCalls := 1
			if mode == "cache" {
				wantExecutableCalls = 0
			}
			if cacheCalls != 1 || executableCalls != wantExecutableCalls {
				t.Fatalf("failed path lookup continued: cache=%d executable=%d", cacheCalls, executableCalls)
			}
		})
	}
}

func TestAttachEntryRejectsUntrustedInheritedChannelsBeforeOpeningAWindow(t *testing.T) {
	if mode := os.Getenv("ZWAI_TEST_RECOVERY_ENTRY"); mode != "" {
		if mode == "closed-ready" {
			if err := os.NewFile(3, "owned ready descriptor").Close(); err != nil {
				t.Fatal(err)
			}
		}
		err := runDesktopAttach(nil)
		want := "desktop-attach requires private inherited pipes"
		if mode == "bad-json" {
			want = "read desktop recovery handoff"
		} else if mode == "bad-lineage" {
			want = "invalid desktop recovery lineage"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("untrusted inherited channel reached native startup: %v", err)
		}
		return
	}
	for _, mode := range []string{"regular-ready", "regular-control", "closed-ready", "bad-json", "bad-lineage"} {
		t.Run(mode, func(t *testing.T) {
			readyRead, readyWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer readyRead.Close()
			defer readyWrite.Close()
			controlRead, controlWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer controlRead.Close()
			defer controlWrite.Close()
			body := "{"
			if mode == "bad-lineage" {
				body = `{ "session_id": "owned", "token": "owned", "generation": 1, "attempts": 3 }`
			}
			if _, err := io.WriteString(controlWrite, body); err != nil {
				t.Fatal(err)
			}
			_ = controlWrite.Close()
			regular, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			defer regular.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAttachEntryRejectsUntrustedInheritedChannelsBeforeOpeningAWindow$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_ENTRY="+mode)
			if coverDir := os.Getenv("GOCOVERDIR"); coverDir != "" {
				command.Args = append(command.Args, "-test.gocoverdir="+coverDir)
			}
			command.ExtraFiles = []*os.File{readyWrite, controlRead}
			if mode == "regular-ready" {
				command.ExtraFiles[0] = regular
			} else if mode == "regular-control" {
				command.ExtraFiles[1] = regular
			}
			var output bytes.Buffer
			command.Stdout, command.Stderr = &output, &output
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			_ = readyWrite.Close()
			_ = controlRead.Close()
			if err := command.Wait(); err != nil {
				t.Fatalf("inherited-channel refusal %s: %v\n%s", mode, err, output.String())
			}
			if receipt, err := io.ReadAll(readyRead); err != nil || len(receipt) != 0 {
				t.Fatalf("invalid channel emitted a renderer-ready receipt: %q %v", receipt, err)
			}
		})
	}
}

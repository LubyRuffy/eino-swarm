//go:build darwin

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Turn admission needs actual kernel facts even when no executable is on PATH.
// The Darwin implementation must not fork merely to collect these facts.
func TestDarwinKernelFactsDoNotRequireUname(t *testing.T) {
	kind, err := syscall.Sysctl("kern.ostype")
	if err != nil {
		t.Fatal(err)
	}
	release, err := syscall.Sysctl("kern.osrelease")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(strings.Fields(kind+" "+release), " ")
	if want == "" {
		t.Fatal("the live kernel fixture returned no facts")
	}
	t.Setenv("PATH", t.TempDir())
	if got := probeKernel(); got != want {
		t.Errorf("kernel facts=%q; want live system facts %q without uname", got, want)
	}
	if prompt := HostEnvironmentPrompt(); !strings.Contains(prompt, ", "+want+"\n") {
		t.Error("the host prompt omitted the live kernel facts when uname is unavailable")
	}
}

func TestDarwinKernelReaderPreservesFailuresAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, failAt              string
		cancelBefore, cancelAfter bool
		wantCalls                 []string
	}{
		{name: "live values", wantCalls: []string{"kern.ostype", "kern.osrelease"}},
		{name: "OS type unavailable", failAt: "kern.ostype", wantCalls: []string{"kern.ostype"}},
		{name: "release unavailable", failAt: "kern.osrelease", wantCalls: []string{"kern.ostype", "kern.osrelease"}},
		{name: "already cancelled", cancelBefore: true},
		{name: "cancelled during read", cancelAfter: true, wantCalls: []string{"kern.ostype", "kern.osrelease"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelBefore {
				cancel()
			}
			failure := errors.New("system query failed")
			var calls []string
			out, err := readDarwinKernel(ctx, func(name string) (string, error) {
				calls = append(calls, name)
				if name == tc.failAt {
					return "", failure
				}
				if name == "kern.ostype" {
					return "FixtureOS", nil
				}
				if tc.cancelAfter {
					cancel()
				}
				return "FixtureRelease", nil
			})
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("queries=%v; want %v", calls, tc.wantCalls)
			}
			switch {
			case tc.cancelBefore || tc.cancelAfter:
				if !errors.Is(err, context.Canceled) || out != nil {
					t.Fatalf("cancelled query=%q err=%v", out, err)
				}
			case tc.failAt != "":
				if !errors.Is(err, failure) || out != nil {
					t.Fatalf("failed query=%q err=%v", out, err)
				}
			default:
				if err != nil || string(out) != "FixtureOS FixtureRelease" {
					t.Fatalf("kernel facts=%q err=%v", out, err)
				}
			}
		})
	}
}

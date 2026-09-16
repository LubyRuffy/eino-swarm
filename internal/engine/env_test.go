package engine

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentPromptNamesTheLiveHost(t *testing.T) {
	got := HostEnvironmentPrompt()
	if !strings.Contains(got, goruntime.GOOS) {
		t.Fatalf("prompt does not name this OS %q:\n%s", goruntime.GOOS, got)
	}
	if !strings.Contains(got, goruntime.GOARCH) {
		t.Fatalf("prompt does not name this arch %q:\n%s", goruntime.GOARCH, got)
	}
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(got, today) {
		t.Fatalf("prompt does not name today's date %s:\n%s", today, got)
	}
	if sh := hostShell(); sh != "" && !strings.Contains(got, sh) {
		t.Fatalf("prompt does not name the live shell %q:\n%s", sh, got)
	}
}

func TestEnvironmentPromptFollowsTheFactsItWasGiven(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, loc)
	got := formatEnvironment(hostFacts{
		OS:     "linux",
		Arch:   "amd64",
		Family: "Linux",
		Kernel: "Linux 6.8.0",
		Shell:  "/bin/bash",
		User:   "dev",
		Home:   "/home/dev",
		Lang:   "C.UTF-8",
	}, now)

	for _, want := range []string{
		"## Environment",
		"Linux (linux/amd64)",
		"Linux 6.8.0",
		"/bin/bash",
		"Wednesday, 16 September 2026",
		"2026-09-16",
		"CST (UTC+08:00)",
		"dev",
		"/home/dev",
		"C.UTF-8",
		"GNU userland",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, leak := range []string{"macOS", "darwin", "find", "-printf"} {
		if strings.Contains(got, leak) {
			t.Fatalf("a linux host must not mention %q:\n%s", leak, got)
		}
	}
}

func TestEnvironmentPromptMarksBSDUserlandOnDarwin(t *testing.T) {
	got := formatEnvironment(hostFacts{
		OS: "darwin", Arch: "arm64", Family: "macOS", Shell: "/bin/zsh",
	}, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "macOS (darwin/arm64)") {
		t.Fatalf("darwin facts were not named:\n%s", got)
	}
	if !strings.Contains(got, "BSD userland") || strings.Contains(got, "GNU userland") {
		t.Fatalf("darwin must be described as BSD, not GNU:\n%s", got)
	}
	if strings.Contains(got, "find") || strings.Contains(got, "-printf") {
		t.Fatal("the motivating command must not leak into the prompt")
	}
}

func TestEnvironmentPromptOmitsBlankOptionalLines(t *testing.T) {
	got := formatEnvironment(hostFacts{OS: "plan9", Arch: "amd64", Family: "plan9"}, time.Unix(0, 0).UTC())
	for _, label := range []string{"- Shell:", "- User:", "- Home:", "- Locale:", "- Kernel"} {
		if strings.Contains(got, label) {
			t.Fatalf("blank fact still rendered %q:\n%s", label, got)
		}
	}
	if !strings.Contains(got, "command syntax and flags this OS and shell actually support") {
		t.Fatalf("unknown OS needs the generic hint:\n%s", got)
	}
}

func TestUserlandHintCoversWindows(t *testing.T) {
	got := userlandHint("windows")
	if !strings.Contains(got, "not Unix-only") {
		t.Fatalf("windows hint=%q", got)
	}
}

func TestFormatTimezoneHandlesNegativeOffsetsAndNamelessZones(t *testing.T) {
	est := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("EST", -5*3600))
	if got := formatTimezone(est); got != "EST (UTC-05:00)" {
		t.Fatalf("negative offset: %q", got)
	}
	nameless := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 0))
	if got := formatTimezone(nameless); got != "UTC+00:00" {
		t.Fatalf("nameless zone: %q", got)
	}
	local := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("Local", 3600))
	if got := formatTimezone(local); got != "UTC+01:00" {
		t.Fatalf("Local is not a timezone name: %q", got)
	}
}

func TestOsFamilyFallsBackToGOOS(t *testing.T) {
	if got := osFamily("aix"); got != "aix" {
		t.Fatalf("got %q", got)
	}
	if osFamily("ios") != "iOS" || osFamily("android") != "Android" {
		t.Fatal("mobile GOOS names should be readable")
	}
}

func TestHostLookupsDoNotPanic(t *testing.T) {
	if hostShell() == "" {
		t.Fatal("every platform has a shell fallback")
	}
	_ = hostUser()
	_ = probeKernel()
	_ = firstEnv()
}

func TestUnameIsSkippedOnWindows(t *testing.T) {
	if unameSupported("windows") {
		t.Fatal("windows has no uname")
	}
	if !unameSupported("darwin") || !unameSupported("linux") {
		t.Fatal("unix hosts should probe uname")
	}
	if kernelRelease("windows") != "" {
		t.Fatal("windows must not emit a kernel line")
	}
}

func TestProbeKernelSwallowsFailure(t *testing.T) {
	orig := probeUname
	probeUname = func(context.Context) ([]byte, error) { return nil, errors.New("missing") }
	t.Cleanup(func() { probeUname = orig })
	if !unameSupported(goruntime.GOOS) {
		if probeKernel() != "" {
			t.Fatal("unsupported GOOS must omit the kernel line")
		}
		return
	}
	if probeKernel() != "" {
		t.Fatal("a failed uname must omit the kernel, not crash the prompt")
	}
}

func TestProbeKernelJoinsFields(t *testing.T) {
	orig := probeUname
	probeUname = func(context.Context) ([]byte, error) { return []byte("  Darwin   25.6.0 \n"), nil }
	t.Cleanup(func() { probeUname = orig })
	if !unameSupported(goruntime.GOOS) {
		if probeKernel() != "" {
			t.Fatal("unsupported GOOS must not call uname")
		}
		return
	}
	if got := probeKernel(); got != "Darwin 25.6.0" {
		t.Fatalf("got %q", got)
	}
}

func TestOsFamilyNamesCommonSystems(t *testing.T) {
	want := map[string]string{
		"darwin": "macOS", "linux": "Linux", "windows": "Windows",
		"freebsd": "FreeBSD", "openbsd": "OpenBSD", "netbsd": "NetBSD",
	}
	for goos, family := range want {
		if got := osFamily(goos); got != family {
			t.Fatalf("osFamily(%q)=%q want %q", goos, got, family)
		}
	}
}

func TestUserlandHintTreatsBSDFamilyTheSame(t *testing.T) {
	for _, goos := range []string{"freebsd", "openbsd", "netbsd", "dragonfly"} {
		if got := userlandHint(goos); !strings.Contains(got, "BSD userland") {
			t.Fatalf("%s hint=%q", goos, got)
		}
	}
	if got := userlandHint("android"); !strings.Contains(got, "GNU userland") {
		t.Fatalf("android hint=%q", got)
	}
}

func TestShellForWindowsFallsBackToCmd(t *testing.T) {
	t.Setenv("ComSpec", "")
	t.Setenv("COMSPEC", "")
	if got := shellFor("windows"); got != "cmd.exe" {
		t.Fatalf("empty COMSPEC: %q", got)
	}
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	if got := shellFor("windows"); got != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("COMSPEC: %q", got)
	}
}

func TestShellForUnixFallsBackToSh(t *testing.T) {
	t.Setenv("SHELL", "")
	if got := shellFor("linux"); got != "/bin/sh" {
		t.Fatalf("got %q", got)
	}
}

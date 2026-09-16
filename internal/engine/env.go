package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"
)

// HostEnvironmentPrompt is the live host snapshot injected into the manager's
// system prompt and every sub-agent's instruction. Generated at call time so
// the date is today, not whatever day the process started. Without it, models
// emit GNU-only flags on BSD userland and dates from their training cutoff.
func HostEnvironmentPrompt() string {
	return formatEnvironment(currentHost(), time.Now())
}

type hostFacts struct {
	OS     string
	Arch   string
	Family string
	Kernel string
	Shell  string
	User   string
	Home   string
	Lang   string
}

func currentHost() hostFacts {
	home, _ := os.UserHomeDir()
	return hostFacts{
		OS:     goruntime.GOOS,
		Arch:   goruntime.GOARCH,
		Family: osFamily(goruntime.GOOS),
		Kernel: probeKernel(),
		Shell:  hostShell(),
		User:   hostUser(),
		Home:   strings.TrimSpace(home),
		Lang:   firstEnv("LANG", "LC_ALL"),
	}
}

func formatEnvironment(h hostFacts, now time.Time) string {
	var b strings.Builder
	b.WriteString("## Environment\n\n")
	b.WriteString(`The tools run on this machine. Use these facts when choosing
commands, flags, paths and dates; do not probe for them.

`)

	osLine := fmt.Sprintf("- OS: %s (%s/%s)", h.Family, h.OS, h.Arch)
	if k := strings.TrimSpace(h.Kernel); k != "" {
		osLine += ", " + k
	}
	b.WriteString(osLine)
	b.WriteString("\n")
	if h.Shell != "" {
		fmt.Fprintf(&b, "- Shell: %s\n", h.Shell)
	}
	fmt.Fprintf(&b, "- Date: %s (%s)\n", now.Format("Monday, 2 January 2006"), now.Format("2006-01-02"))
	if tz := formatTimezone(now); tz != "" {
		fmt.Fprintf(&b, "- Timezone: %s\n", tz)
	}
	if h.User != "" {
		fmt.Fprintf(&b, "- User: %s\n", h.User)
	}
	if h.Home != "" {
		fmt.Fprintf(&b, "- Home: %s\n", h.Home)
	}
	if h.Lang != "" {
		fmt.Fprintf(&b, "- Locale: %s\n", h.Lang)
	}
	b.WriteString("\n")
	b.WriteString(userlandHint(h.OS))
	b.WriteString("\n")
	return b.String()
}

func osFamily(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	default:
		return goos
	}
}

func userlandHint(goos string) string {
	switch goos {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		return "This is BSD userland, not GNU. Use the flags and utilities this OS actually ships.\n"
	case "linux", "android":
		return "This is GNU userland. Use the flags and utilities this OS actually ships.\n"
	case "windows":
		return "Commands run through that shell. Use this OS's command syntax, not Unix-only flags.\n"
	default:
		return "Use the command syntax and flags this OS and shell actually support.\n"
	}
}

func hostShell() string { return shellFor(goruntime.GOOS) }

func shellFor(goos string) string {
	if goos == "windows" {
		if v := firstEnv("ComSpec", "COMSPEC"); v != "" {
			return v
		}
		return "cmd.exe"
	}
	if v := firstEnv("SHELL"); v != "" {
		return v
	}
	return "/bin/sh"
}

func hostUser() string {
	return firstEnv("USER", "USERNAME", "LOGNAME")
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func formatTimezone(now time.Time) string {
	name, offset := now.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	utc := fmt.Sprintf("UTC%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
	if name == "" || name == "Local" {
		return utc
	}
	return name + " (" + utc + ")"
}

func unameSupported(goos string) bool { return goos != "windows" }

// probeUname is the uname invocation so a test can fail it without a real binary.
var probeUname = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "uname", "-s", "-r").Output()
}

func probeKernel() string { return kernelRelease(goruntime.GOOS) }

func kernelRelease(goos string) string {
	if !unameSupported(goos) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := probeUname(ctx)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

package update

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// ReleaseRepo is the GitHub project whose Releases feed the desktop and
// the phone. It is the product channel, not a setting: a config field
// here would let a rewritten file point the installer at another repo.
const ReleaseRepo = "LubyRuffy/eino-swarm"

// LatestURL is the public feed. Drafts are not "latest".
func LatestURL() string {
	return "https://api.github.com/repos/" + ReleaseRepo + "/releases/latest"
}

// DarwinAssetName is the zip attached to a release for one Mac arch.
// The zip's root is zwai.app.
func DarwinAssetName(version, arch string) string {
	return "zwai-" + Canonical(version) + "-darwin-" + arch + ".zip"
}

// Canonical strips a leading v and keeps the numeric triple, or "" when
// the string is not a release version. A dirty git describe is not one.
func Canonical(raw string) string {
	parsed := Parse(raw)
	if parsed == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", parsed[0], parsed[1], parsed[2])
}

// Parse reads a leading major.minor.patch. Anything after the triple
// (a pre-release, a git suffix) is not a release we can compare.
func Parse(raw string) *[3]int {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	var out [3]int
	for i, part := range parts {
		if part == "" || strings.TrimLeft(part, "0123456789") != "" {
			return nil
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return &out
}

// Newer reports whether latest is a higher numeric triple than current.
func Newer(latest, current string) bool {
	next := Parse(latest)
	have := Parse(current)
	if next == nil || have == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if next[i] != have[i] {
			return next[i] > have[i]
		}
	}
	return false
}

// HostArch is the Mac cpu this process can install. Other systems have none.
func HostArch() string {
	return archFor(runtime.GOOS, runtime.GOARCH)
}

func archFor(goos, goarch string) string {
	if goos != "darwin" {
		return ""
	}
	switch goarch {
	case "arm64", "amd64":
		return goarch
	default:
		return ""
	}
}

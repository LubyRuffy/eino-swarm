//go:build darwin

package desktop

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	macBundleID   = "app.zwai.desktop"
	macBundleName = "zwai"
)

// ReexecIfUnbundled copies this process into a stable .app and replaces
// the image with that copy. `go run` leaves a naked executable (Info.plist
// not bound, identifier a.out); macOS Local Network privacy then returns
// EHOSTUNREACH for LAN model endpoints that Terminal's curl can reach.
// syscall.Exec never returns on success.
func ReexecIfUnbundled() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	bundled, err := prepareBundle(exe, cache)
	if err != nil {
		return err
	}
	if bundled == "" {
		return nil
	}
	return syscall.Exec(bundled, append([]string{bundled}, os.Args[1:]...), os.Environ())
}

func prepareBundle(exe, cacheDir string) (string, error) {
	if alreadyBundled(exe) {
		return "", nil
	}
	appDir := filepath.Join(cacheDir, "zwai", "zwai.app")
	if err := writeAppBundle(exe, appDir, ""); err != nil {
		return "", fmt.Errorf("desktop: local-network app bundle: %w", err)
	}
	return filepath.Join(appDir, "Contents", "MacOS", macBundleName), nil
}

func alreadyBundled(exe string) bool {
	return strings.Contains(filepath.ToSlash(filepath.Clean(exe)), ".app/Contents/MacOS/")
}

// WriteMacApp copies exe into a zwai.app whose short version is the
// release triple. An empty version keeps the cache bundle's placeholder.
func WriteMacApp(exe, appDir, version string) error {
	return writeAppBundle(exe, appDir, version)
}

func writeAppBundle(exe, appDir, version string) error {
	if version != "" && strings.Trim(version, "0123456789.") != "" {
		return fmt.Errorf("desktop: version %q is not a release triple", version)
	}
	macOSDir := filepath.Join(appDir, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "Contents", "Info.plist"), []byte(macInfoPlist(version)), 0o644); err != nil {
		return err
	}
	dest := filepath.Join(macOSDir, macBundleName)
	if err := copyFile(exe, dest); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}
	// Best-effort: a copied signature is invalid. Ad-hoc sign so TCC can
	// key off the bundle id instead of a.out.
	_ = exec.Command("codesign", "--force", "--sign", "-", "--identifier", macBundleID, dest).Run()
	return nil
}

func macInfoPlist(version string) string {
	if version == "" {
		version = "0.1"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>` + macBundleName + `</string>
	<key>CFBundleIdentifier</key>
	<string>` + macBundleID + `</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>` + macBundleName + `</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + version + `</string>
	<key>CFBundleVersion</key>
	<string>` + version + `</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSLocalNetworkUsageDescription</key>
	<string>zwai lists models and talks to endpoints on your local network.</string>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSAllowsLocalNetworking</key>
		<true/>
	</dict>
</dict>
</plist>
`
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

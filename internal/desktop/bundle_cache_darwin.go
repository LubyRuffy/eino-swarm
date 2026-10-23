//go:build darwin

package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type cachedBundleReceipt struct {
	Source     string `json:"source_sha256"`
	Executable string `json:"executable_sha256"`
	Plist      string `json:"plist_sha256"`
	Icon       string `json:"icon_sha256"`
}

func prepareImmutableBundle(exe, cacheDir string) (string, error) {
	source, err := bundleFileHash(exe)
	if err != nil {
		return "", err
	}
	root := filepath.Join(cacheDir, "zwai", "bundles")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(root, source+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	final := filepath.Join(root, source)
	if _, err := os.Lstat(final); err == nil {
		return verifyCachedBundle(final, source)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temporary, err := os.MkdirTemp(root, ".prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	app := filepath.Join(temporary, "zwai.app")
	if err := writeCacheBundle(exe, app, ""); err != nil {
		return "", fmt.Errorf("desktop: local-network app bundle: %w", err)
	}
	// A changing source may not publish a completed identity under the first hash.
	current, err := bundleFileHash(exe)
	if err != nil {
		return "", err
	}
	if current != source {
		return "", fmt.Errorf("desktop executable changed during bundle preparation")
	}
	files := []string{filepath.Join(app, "Contents", "MacOS", macBundleName), filepath.Join(app, "Contents", "Info.plist"), filepath.Join(app, "Contents", "Resources", "AppIcon.icns")}
	hashes := make([]string, len(files))
	for i, path := range files {
		hashes[i], err = bundleFileHash(path)
		if err != nil {
			return "", err
		}
	}
	receipt := cachedBundleReceipt{Source: source, Executable: hashes[0], Plist: hashes[1], Icon: hashes[2]}
	body, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(temporary, "complete.json"), body, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, final); err != nil {
		return "", err
	}
	return verifyCachedBundle(final, source)
}

func verifyCachedBundle(directory, source string) (string, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("desktop immutable bundle is not a directory")
	}
	body, err := os.ReadFile(filepath.Join(directory, "complete.json"))
	if err != nil {
		return "", fmt.Errorf("desktop immutable bundle is incomplete: %w", err)
	}
	var receipt cachedBundleReceipt
	if json.Unmarshal(body, &receipt) != nil || receipt.Source != source {
		return "", fmt.Errorf("desktop immutable bundle receipt does not match its source")
	}
	app := filepath.Join(directory, "zwai.app", "Contents")
	executable := filepath.Join(app, "MacOS", macBundleName)
	for _, file := range []struct{ path, hash string }{{executable, receipt.Executable}, {filepath.Join(app, "Info.plist"), receipt.Plist}, {filepath.Join(app, "Resources", "AppIcon.icns"), receipt.Icon}} {
		actual, err := bundleFileHash(file.path)
		if err != nil {
			return "", err
		}
		if actual != file.hash {
			return "", fmt.Errorf("desktop immutable bundle content changed")
		}
	}
	return executable, nil
}

func bundleFileHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("desktop bundle file must be regular")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

package update

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxZipBytes  = 512 << 20
	maxZipFiles  = 20000
	bundleMarker = ".app/Contents/MacOS/"
)

var (
	errNotInstalled = errors.New("desktop updates replace an installed zwai.app")
	errNoAsset      = errors.New("this release has no macOS build")
	errBadArchive   = errors.New("the downloaded update is not a zwai.app")
)

// Apply downloads the offered version and swaps it over the running app.
// desktopPIDs are window processes to close after the new app is opened.
func (s *Service) Apply(ctx context.Context, version string, desktopPIDs []int) error {
	want := Canonical(version)
	if want == "" {
		return errors.New("missing version")
	}
	result := s.Check(ctx, true)
	if result.Status != "available" || result.Offer == nil || result.Offer.Version != want {
		if result.Message != "" {
			return errors.New(result.Message)
		}
		return errNoAsset
	}
	exe, err := s.executable()
	if err != nil {
		return err
	}
	app, err := InstalledApp(exe)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "zwai-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	zipPath := filepath.Join(dir, "update.zip")
	if err := s.download(ctx, result.Offer.AssetURL, zipPath); err != nil {
		return err
	}
	if err := unzipApp(zipPath, filepath.Join(dir, "unpack")); err != nil {
		return err
	}
	src := filepath.Join(dir, "unpack", "zwai.app")
	if err := swapApp(src, app); err != nil {
		return err
	}
	if err := s.open(app); err != nil {
		return err
	}
	s.signal(desktopPIDs)
	s.retirePrevious(app)
	return nil
}

func (s *Service) executable() (string, error) {
	if s.Exe != nil {
		return s.Exe()
	}
	return os.Executable()
}

func (s *Service) open(app string) error {
	if s.Open != nil {
		return s.Open(app)
	}
	return exec.Command("open", app).Start()
}

func (s *Service) signal(pids []int) {
	fn := s.Signal
	if fn == nil {
		fn = signalPID
	}
	seen := map[int]bool{}
	for _, pid := range pids {
		if pid <= 0 || pid == os.Getpid() || seen[pid] {
			continue
		}
		seen[pid] = true
		fn(pid)
	}
}

func (s *Service) retirePrevious(app string) {
	previous := app + ".previous"
	if _, err := os.Stat(previous); err != nil {
		return
	}
	script := fmt.Sprintf("#!/bin/sh\nwhile kill -0 %d 2>/dev/null; do sleep 0.2; done\nrm -rf %q\n", os.Getpid(), previous)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("zwai-update-cleanup-%d.sh", os.Getpid()))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return
	}
	spawn := s.Spawn
	if spawn == nil {
		spawn = spawnDetached
	}
	if err := spawn(path); err != nil {
		_ = os.Remove(path)
	}
}

func spawnDetached(script string) error {
	cmd := exec.Command("sh", script)
	return cmd.Start()
}

// InstalledApp is the zwai.app that contains this executable.
func InstalledApp(exe string) (string, error) {
	clean := filepath.Clean(exe)
	slash := filepath.ToSlash(clean)
	i := strings.Index(slash, bundleMarker)
	if i < 0 {
		return "", errNotInstalled
	}
	if filepath.Base(clean) != "zwai" {
		return "", errNotInstalled
	}
	return filepath.FromSlash(slash[:i] + ".app"), nil
}

func (s *Service) download(ctx context.Context, rawURL, dest string) error {
	if AllowedDownload(rawURL) == "" {
		return errNoAsset
	}
	res, err := s.fetch(ctx, rawURL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		msg := githubMessage(raw)
		if msg == "" {
			msg = res.Status
		}
		return errors.New(msg)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(res.Body, maxZipBytes+1))
	if err != nil {
		return err
	}
	if n > maxZipBytes {
		return errors.New("the update is larger than this installer accepts")
	}
	return nil
}

func unzipApp(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return errBadArchive
	}
	defer r.Close()
	if len(r.File) == 0 || len(r.File) > maxZipFiles {
		return errBadArchive
	}
	var wrote int64
	sawBin := false
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if rejectedZipName(name) {
			return errBadArchive
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return errBadArchive
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipFile(f, target, &wrote); err != nil {
			return err
		}
		if name == "zwai.app/Contents/MacOS/zwai" {
			sawBin = true
			if err := os.Chmod(target, 0o755); err != nil {
				return err
			}
		}
	}
	if !sawBin {
		return errBadArchive
	}
	return nil
}

func rejectedZipName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "../") {
		return true
	}
	return name != "zwai.app" && !strings.HasPrefix(name, "zwai.app/")
}

func writeZipFile(f *zip.File, target string, wrote *int64) error {
	in, err := f.Open()
	if err != nil {
		return errBadArchive
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, maxZipBytes+1))
	closeErr := out.Close()
	*wrote += n
	if *wrote > maxZipBytes {
		return errors.New("the update is larger than this installer accepts")
	}
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func swapApp(src, dest string) error {
	info, err := os.Stat(filepath.Join(src, "Contents", "MacOS", "zwai"))
	if err != nil || info.IsDir() {
		return errBadArchive
	}
	incoming := dest + ".incoming"
	previous := dest + ".previous"
	_ = os.RemoveAll(incoming)
	if err := copyTree(src, incoming); err != nil {
		_ = os.RemoveAll(incoming)
		return err
	}
	_ = os.RemoveAll(previous)
	if err := os.Rename(dest, previous); err != nil {
		_ = os.RemoveAll(incoming)
		return err
	}
	if err := os.Rename(incoming, dest); err != nil {
		_ = os.Rename(previous, dest)
		_ = os.RemoveAll(incoming)
		return err
	}
	return nil
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errBadArchive
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
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

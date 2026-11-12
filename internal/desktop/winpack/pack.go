package main

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

const (
	machineAMD64 = 0x8664
	machineARM64 = 0xaa64
)

// placeZip is install. Tests replace it to force a partial publish to roll back.
var placeZip = install

// compileHook is compileWindows. Tests replace it so a nil compiler does not
// cross-compile the whole module.
var compileHook = compileWindows

// runCommand executes the cross-compile. Tests replace it so they can check
// the GOOS/GOARCH command without building the module.
var runCommand = func(cmd *exec.Cmd) error { return cmd.Run() }

// Build cross-compiles zwai.exe for amd64 and arm64 and writes both release
// zips into outDir. compile nil uses the host Go toolchain with CGO disabled.
// A failed architecture leaves no zip from this attempt.
func Build(version, outDir string, compile func(arch, exe string) error) ([]string, error) {
	canonical := update.Canonical(version)
	if canonical == "" {
		return nil, fmt.Errorf("windows release needs a numeric version, got %q", version)
	}
	if compile == nil {
		compile = compileHook(canonical)
	}
	stage, err := os.MkdirTemp("", "zwai-windows-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	var staged []string
	for _, arch := range []string{"amd64", "arm64"} {
		exe := filepath.Join(stage, arch, "zwai.exe")
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			return nil, err
		}
		if err := compile(arch, exe); err != nil {
			return nil, err
		}
		if err := validateExecutable(exe, arch); err != nil {
			return nil, err
		}
		archive := filepath.Join(stage, update.WindowsAssetName(canonical, arch))
		if err := update.ZipWindowsExe(exe, archive); err != nil {
			return nil, err
		}
		if err := ValidateZip(archive, arch); err != nil {
			return nil, err
		}
		staged = append(staged, archive)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var published []string
	for _, src := range staged {
		dest := filepath.Join(outDir, filepath.Base(src))
		if err := placeZip(src, dest); err != nil {
			for _, path := range published {
				_ = os.Remove(path)
			}
			return nil, err
		}
		published = append(published, dest)
	}
	return published, nil
}

func compileWindows(version string) func(arch, exe string) error {
	return func(arch, exe string) error {
		root, err := moduleRoot()
		if err != nil {
			return err
		}
		cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", exe, "./cmd/zwai")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=windows", "GOARCH="+arch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := runCommand(cmd); err != nil {
			return fmt.Errorf("build windows %s: %w", arch, err)
		}
		return nil
	}
}

// ValidateZip accepts one zip whose only file is a zwai.exe of arch.
// The machine field is the same check the release publisher applies.
func ValidateZip(path, arch string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("windows installer is not a zip: %w", err)
	}
	defer reader.Close()
	var files []*zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		files = append(files, file)
	}
	if len(files) != 1 || files[0].Name != "zwai.exe" {
		return fmt.Errorf("windows installer must contain only zwai.exe")
	}
	body, err := files[0].Open()
	if err != nil {
		return err
	}
	defer body.Close()
	exe, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	return validatePE(exe, arch)
}

func validateExecutable(path, arch string) error {
	exe, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return validatePE(exe, arch)
}

func validatePE(exe []byte, arch string) error {
	want, ok := map[string]uint16{"amd64": machineAMD64, "arm64": machineARM64}[arch]
	if !ok {
		return fmt.Errorf("windows release arch must be arm64 or amd64, got %q", arch)
	}
	got, err := peMachine(exe)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("windows %s executable has machine %#x", arch, got)
	}
	return nil
}

func peMachine(exe []byte) (uint16, error) {
	if len(exe) < 0x40 || string(exe[:2]) != "MZ" {
		return 0, fmt.Errorf("windows installer is not a PE executable")
	}
	offset := binary.LittleEndian.Uint32(exe[0x3C:0x40])
	if offset < 0x40 || int(offset)+6 > len(exe) || string(exe[offset:offset+4]) != "PE\x00\x00" {
		return 0, fmt.Errorf("windows installer is not a PE executable")
	}
	return binary.LittleEndian.Uint16(exe[offset+4 : offset+6]), nil
}

func install(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	partial := dest + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return closeErr
	}
	if err := os.Rename(partial, dest); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

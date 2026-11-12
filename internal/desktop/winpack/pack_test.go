package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRejectsAVersionThatIsNotARelease(t *testing.T) {
	if _, err := Build("dev", t.TempDir(), fakeCompile); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatal(err)
	}
	if _, err := Build("1.2.3-dirty", t.TempDir(), fakeCompile); err == nil {
		t.Fatal("a dirty describe is not a release")
	}
}

func TestBuildWritesBothArchitecturesAndRollsBackAPartialPublish(t *testing.T) {
	out := t.TempDir()
	paths, err := Build("v1.2.3", out, fakeCompile)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths %v", paths)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if err := ValidateZip(filepath.Join(out, "zwai-1.2.3-windows-"+arch+".zip"), arch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Build("1.2.3", filepath.Join(out, "missing", "zwai.exe"), failSecond); err == nil {
		t.Fatal("a bad executable was packaged")
	}
	previous := placeZip
	t.Cleanup(func() { placeZip = previous })
	placed := 0
	placeZip = func(src, dest string) error {
		placed++
		if placed == 2 {
			return errors.New("disk full")
		}
		return install(src, dest)
	}
	partial := t.TempDir()
	if _, err := Build("1.2.3", partial, fakeCompile); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial publish left %d files", len(entries))
	}
}

func TestValidateZipRejectsTheWrongShape(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZip(plain, "amd64"); err == nil {
		t.Fatal("a text file was accepted")
	}
	extra := filepath.Join(dir, "extra.zip")
	writeZip(t, extra, map[string][]byte{"zwai.exe": pe(machineAMD64), "readme.txt": []byte("x")})
	if err := ValidateZip(extra, "amd64"); err == nil || !strings.Contains(err.Error(), "only zwai.exe") {
		t.Fatal(err)
	}
	wrong := filepath.Join(dir, "wrong.zip")
	writeZip(t, wrong, map[string][]byte{"zwai.exe": pe(machineARM64)})
	if err := ValidateZip(wrong, "amd64"); err == nil || !strings.Contains(err.Error(), "machine") {
		t.Fatal(err)
	}
	if err := validatePE(pe(machineAMD64), "386"); err == nil {
		t.Fatal("386 was accepted")
	}
	if err := validateExecutable(filepath.Join(dir, "missing.exe"), "amd64"); err == nil {
		t.Fatal("missing executable was accepted")
	}
	short := filepath.Join(dir, "short.zip")
	writeZip(t, short, map[string][]byte{"zwai.exe": []byte("MZ")})
	if err := ValidateZip(short, "amd64"); err == nil {
		t.Fatal("a short executable was accepted")
	}
}

func TestModuleRootAndCrossCompileFailure(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	away := t.TempDir()
	t.Chdir(away)
	if _, err := moduleRoot(); err == nil {
		t.Fatal("temp dir was treated as a module")
	}
	err = compileWindows("1.2.3")("amd64", filepath.Join(away, "zwai.exe"))
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatal(err)
	}
}

func TestNilCompilerUsesTheRealBuilderHook(t *testing.T) {
	previous := compileHook
	t.Cleanup(func() { compileHook = previous })
	compileHook = func(string) func(string, string) error { return fakeCompile }
	paths, err := Build("1.2.3", t.TempDir(), nil)
	if err != nil || len(paths) != 2 {
		t.Fatalf("%v %v", paths, err)
	}
}

func TestRunPrintsTheZipsAndStopsForABadVersion(t *testing.T) {
	previous := compileHook
	t.Cleanup(func() { compileHook = previous })
	compileHook = func(string) func(string, string) error { return fakeCompile }
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-version", "1.2.3", "-o", out}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	if !strings.Contains(text, "zwai-1.2.3-windows-amd64.zip") || !strings.Contains(text, "zwai-1.2.3-windows-arm64.zip") {
		t.Fatal(text)
	}
	if err := run([]string{"-version", "dev", "-o", out}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatal(err)
	}
	stderr.Reset()
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil || !strings.Contains(stderr.String(), "-version") {
		t.Fatalf("%v %s", err, stderr.String())
	}
	if err := run([]string{"-unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
}

func TestBuildStopsBeforePublishingABrokenArchive(t *testing.T) {
	if _, err := Build("1.2.3", t.TempDir(), func(string, string) error {
		return errors.New("toolchain")
	}); err == nil || !strings.Contains(err.Error(), "toolchain") {
		t.Fatal(err)
	}
	if _, err := Build("1.2.3", t.TempDir(), func(_, exe string) error {
		return os.MkdirAll(exe, 0o755)
	}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build("1.2.3", blocked, fakeCompile); err == nil {
		t.Fatal("a file was used as the output directory")
	}
}

func TestDefaultRunCommandRuns(t *testing.T) {
	if err := runCommand(exec.Command("go", "version")); err != nil {
		t.Fatal(err)
	}
}

func TestCompileWindowsUsesTheWindowsCrossCompile(t *testing.T) {
	previous := runCommand
	t.Cleanup(func() { runCommand = previous })
	var got *exec.Cmd
	runCommand = func(cmd *exec.Cmd) error {
		got = cmd
		return nil
	}
	exe := filepath.Join(t.TempDir(), "zwai.exe")
	if err := compileWindows("1.2.3")("arm64", exe); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Args, " ")
	env := strings.Join(got.Env, "\n")
	if got.Dir == "" || !strings.Contains(joined, "-X main.version=1.2.3") || !strings.Contains(joined, exe) || !strings.Contains(joined, "./cmd/zwai") {
		t.Fatalf("%s in %s", joined, got.Dir)
	}
	for _, needle := range []string{"CGO_ENABLED=0", "GOOS=windows", "GOARCH=arm64"} {
		if !strings.Contains(env, needle) {
			t.Fatalf("missing %s in %s", needle, env)
		}
	}
	runCommand = func(*exec.Cmd) error { return errors.New("boom") }
	err := compileWindows("1.2.3")("amd64", exe)
	if err == nil || !strings.Contains(err.Error(), "build windows amd64") {
		t.Fatal(err)
	}
}

func TestValidateZipIgnoresDirectoriesAndRejectsABadMachineHeader(t *testing.T) {
	dir := t.TempDir()
	withDir := filepath.Join(dir, "with-dir.zip")
	writeZip(t, withDir, map[string][]byte{"keep/": nil, "zwai.exe": pe(machineAMD64)})
	if err := ValidateZip(withDir, "amd64"); err != nil {
		t.Fatal(err)
	}
	shifted := make([]byte, 0x40)
	copy(shifted, "MZ")
	binary.LittleEndian.PutUint32(shifted[0x3C:], 0x10)
	bad := filepath.Join(dir, "bad-pe.zip")
	writeZip(t, bad, map[string][]byte{"zwai.exe": shifted})
	if err := ValidateZip(bad, "amd64"); err == nil || !strings.Contains(err.Error(), "not a PE") {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "method.zip")
	writeZip(t, unreadable, map[string][]byte{"zwai.exe": pe(machineAMD64)})
	blob, err := os.ReadFile(unreadable)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob[:4]) != "PK\x03\x04" {
		t.Fatal("zip has no local header")
	}
	binary.LittleEndian.PutUint16(blob[8:10], 99)
	index := bytes.Index(blob, []byte("PK\x01\x02"))
	if index < 0 {
		t.Fatal("zip has no central directory")
	}
	binary.LittleEndian.PutUint16(blob[index+10:index+12], 99)
	if err := os.WriteFile(unreadable, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZip(unreadable, "amd64"); err == nil {
		t.Fatal("an unsupported compression method was read")
	}
}

func TestInstallRemovesAPartialCopy(t *testing.T) {
	dir := t.TempDir()
	if err := install(filepath.Join(dir, "missing"), filepath.Join(dir, "out.zip")); err == nil {
		t.Fatal("missing source was copied")
	}
	src := filepath.Join(dir, "src.zip")
	if err := os.WriteFile(src, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install(src, filepath.Join(dir, "absent", "out.zip")); err == nil {
		t.Fatal("missing destination directory was created by the copy")
	}
	folder := filepath.Join(dir, "folder")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, "copied.zip")
	if err := install(folder, copied); err == nil {
		t.Fatal("a directory was copied")
	}
	if _, err := os.Stat(copied + ".partial"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := install(src, dest); err == nil {
		t.Fatal("a zip replaced a directory")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestModuleRootReportsADeletedWorkingDirectory(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if err := os.Remove(nested); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleRoot(); err == nil {
		t.Fatal("a deleted working directory was readable")
	}
}

func fakeCompile(arch, exe string) error {
	machine := uint16(machineAMD64)
	if arch == "arm64" {
		machine = machineARM64
	}
	if arch != "amd64" && arch != "arm64" {
		return errors.New("arch")
	}
	return os.WriteFile(exe, pe(machine), 0o755)
}

func failSecond(arch, exe string) error {
	if arch == "arm64" {
		return os.WriteFile(exe, []byte("not a pe"), 0o755)
	}
	return fakeCompile(arch, exe)
}

func pe(machine uint16) []byte {
	buf := make([]byte, 0x80)
	copy(buf, "MZ")
	binary.LittleEndian.PutUint32(buf[0x3C:], 0x40)
	copy(buf[0x40:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(buf[0x44:], machine)
	return buf
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

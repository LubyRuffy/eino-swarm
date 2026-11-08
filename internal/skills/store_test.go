package skills

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const portableSkillFixture = "---\r\nname: verify-layout\r\ndescription: >-\r\n  Check package layout\r\n  before review.\r\nmetadata:\r\n  author: example\r\n---\r\n\r\nRead [notes](references/notes.md) before continuing.\r\n"

type skillZIPEntry struct {
	name string
	body string
	mode fs.FileMode
}

func makeSkillZIP(t *testing.T, entries ...skillZIPEntry) *zip.Reader {
	t.Helper()
	var encoded bytes.Buffer
	writer := zip.NewWriter(&encoded)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		w, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(encoded.Bytes()), int64(encoded.Len()))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		t.Fatal(err)
	}
	return reader
}

func TestInstalledSkillPreservesItsWholePortableBundle(t *testing.T) {
	for _, prefix := range []string{"", "verify-layout/"} {
		t.Run(prefix+"bundle", func(t *testing.T) {
			s := New(t.TempDir())
			archive := makeSkillZIP(t,
				skillZIPEntry{prefix + "SKILL.md", portableSkillFixture, 0o644},
				skillZIPEntry{prefix + "references/", "", fs.ModeDir | 0o755},
				skillZIPEntry{prefix + "references/notes.md", "private reference\r\n", 0o644},
				skillZIPEntry{prefix + "scripts/check", "#!/bin/sh\nexit 42\n", 0o755},
				skillZIPEntry{prefix + "assets/template.bin", "\x00\x01\xff", 0o644},
			)
			info, err := s.InstallSkillZIP(t.Context(), archive)
			if err != nil || info.Name != "verify-layout" ||
				info.Description != "Check package layout before review." || info.UpdatedAt.IsZero() {
				t.Fatalf("install = %+v, %v", info, err)
			}
			fresh := New(s.projectDir)
			all, err := fresh.ListInstalledSkills()
			if err != nil || len(all) != 1 || all[0] != info {
				t.Fatalf("durable index = %+v, %v", all, err)
			}
			skill, err := fresh.ReadInstalledSkill(info.Name)
			if err != nil || !filepath.IsAbs(skill.BaseDir) ||
				skill.Body != "Read [notes](references/notes.md) before continuing." {
				t.Fatalf("read = %+v, %v", skill, err)
			}
			for _, file := range archive.File {
				if file.Mode().IsDir() {
					continue
				}
				var want string
				switch strings.TrimPrefix(file.Name, prefix) {
				case "SKILL.md":
					want = portableSkillFixture
				case "references/notes.md":
					want = "private reference\r\n"
				case "scripts/check":
					want = "#!/bin/sh\nexit 42\n"
				case "assets/template.bin":
					want = "\x00\x01\xff"
				}
				dst := filepath.Join(skill.BaseDir, filepath.FromSlash(strings.TrimPrefix(file.Name, prefix)))
				got, err := os.ReadFile(dst)
				if err != nil || string(got) != want {
					t.Fatalf("bundle file %s = %q, %v", dst, got, err)
				}
				if runtime.GOOS != "windows" {
					mode, err := os.Stat(dst)
					if err != nil || mode.Mode().Perm() != 0o600|file.Mode().Perm()&0o100 {
						t.Fatalf("file mode %s = %v, %v", dst, mode, err)
					}
				}
			}
			if err := fresh.RemoveInstalledSkill(info.Name); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(skill.BaseDir); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("bundle survived uninstall: %v", err)
			}
			if all, err := fresh.ListInstalledSkills(); err != nil || len(all) != 0 {
				t.Fatalf("removed bundle still visible: %+v, %v", all, err)
			}
			if err := fresh.RemoveInstalledSkill(info.Name); !errors.Is(err, ErrNoMatch) {
				t.Fatalf("uninstall missing = %v", err)
			}
		})
	}
}

func TestSkillInstallRejectsUnsafeOrIncompleteBundlesWithoutPublishing(t *testing.T) {
	manifest := skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644}
	cases := map[string][]skillZIPEntry{
		"missing manifest":          {{"reference.md", "text", 0o644}},
		"traversal":                 {manifest, {"../escaped", "outside", 0o644}},
		"absolute":                  {manifest, {"/outside", "outside", 0o644}},
		"backslash":                 {manifest, {`references\notes.md`, "unexpected", 0o644}},
		"drive or alternate stream": {manifest, {"reference.md:stream", "unexpected", 0o644}},
		"trailing dot":              {manifest, {"assets/invalid.", "text", 0o644}},
		"dirty path":                {manifest, {"references/./notes.md", "text", 0o644}},
		"symlink":                   {manifest, {"references/notes.md", "SKILL.md", fs.ModeSymlink | 0o777}},
		"special":                   {manifest, {"references/device", "", fs.ModeDevice | 0o600}},
		"duplicate":                 {manifest, manifest},
		"case alias":                {manifest, {"references/a", "one", 0o644}, {"References/b", "two", 0o644}},
		"file ancestor":             {manifest, {"references", "file", 0o644}, {"references/a", "text", 0o644}},
		"mixed roots":               {{"wrapper/SKILL.md", portableSkillFixture, 0o644}, {"other/notes.md", "text", 0o644}},
		"two wrapper manifests":     {{"one/SKILL.md", portableSkillFixture, 0o644}, {"two/SKILL.md", portableSkillFixture, 0o644}},
		"deep wrapper":              {{"one/two/SKILL.md", portableSkillFixture, 0o644}},
		"invalid YAML":              {{"SKILL.md", "---\nname: [\n---\ntext", 0o644}},
		"untyped fields":            {{"SKILL.md", "---\nname: 7\ndescription: false\n---\ntext", 0o644}},
		"duplicate YAML name":       {{"SKILL.md", "---\nname: first\nname: second\ndescription: text\n---\nbody", 0o644}},
		"missing description":       {{"SKILL.md", "---\nname: verify-layout\n---\ntext", 0o644}},
		"bad name":                  {{"SKILL.md", strings.Replace(portableSkillFixture, "verify-layout", "../other", 1), 0o644}},
		"double hyphen":             {{"SKILL.md", strings.Replace(portableSkillFixture, "verify-layout", "verify--layout", 1), 0o644}},
		"empty body":                {{"SKILL.md", "---\nname: verify-layout\ndescription: text\n---\n\n", 0o644}},
		"bad encoding":              {{"SKILL.md", portableSkillFixture + "\xff", 0o644}},
		"long description":          {{"SKILL.md", "---\nname: verify-layout\ndescription: " + strings.Repeat("中", 1025) + "\n---\ntext", 0o644}},
		"oversize manifest":         {{"SKILL.md", portableSkillFixture + strings.Repeat("x", 256<<10), 0o644}},
	}
	for label, entries := range cases {
		t.Run(label, func(t *testing.T) {
			s := New(t.TempDir())
			_, err := s.InstallSkillZIP(t.Context(), makeSkillZIP(t, entries...))
			if !errors.Is(err, ErrInvalidSkillBundle) {
				t.Fatalf("install invalid bundle = %v", err)
			}
			if got, err := s.ListInstalledSkills(); err != nil || len(got) != 0 {
				t.Fatalf("invalid bundle left visible skill: %+v, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(s.projectDir, "escaped")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("unsafe archive wrote outside stage: %v", err)
			}
		})
	}
}

func TestSkillInstallChecksBudgetsAndCancellationBeforePublication(t *testing.T) {
	s := New(t.TempDir())
	manifest := skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644}
	entries := []skillZIPEntry{manifest}
	for i := range 512 {
		entries = append(entries, skillZIPEntry{fmt.Sprintf("assets/%d", i), "", 0o644})
	}
	for label, archive := range map[string]*zip.Reader{
		"too many entries": makeSkillZIP(t, entries...),
		"too many bytes":   makeSkillZIP(t, manifest, skillZIPEntry{"assets/huge", strings.Repeat("x", 32<<20), 0o644}),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := s.InstallSkillZIP(t.Context(), archive); !errors.Is(err, ErrInvalidSkillBundle) {
				t.Fatalf("install = %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.InstallSkillZIP(ctx, makeSkillZIP(t, manifest)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled install = %v", err)
	}
	if got, err := s.ListInstalledSkills(); err != nil || len(got) != 0 {
		t.Fatalf("failed install left visible skill: %+v, %v", got, err)
	}
	missing := New(filepath.Join(s.projectDir, "removed-project"))
	if _, err := missing.InstallSkillZIP(t.Context(), makeSkillZIP(t, manifest)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("install against removed project = %v", err)
	}
	if _, err := os.Stat(missing.projectDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("install recreated removed project: %v", err)
	}
}

func TestInstalledSkillNamesIdentifyOnlyOnePackage(t *testing.T) {
	archive := makeSkillZIP(t, skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644})
	s := New(t.TempDir())
	const name = "verify-layout"
	if _, err := s.InstallSkillZIP(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallSkillZIP(t.Context(), archive); !errors.Is(err, ErrSkillInstallConflict) {
		t.Fatalf("reinstall = %v", err)
	}
	if _, err := s.ReadInstalledSkill("../other"); !errors.Is(err, ErrBadName) {
		t.Fatalf("read unsafe skill name = %v", err)
	}
	if err := s.RemoveInstalledSkill("../other"); !errors.Is(err, ErrBadName) {
		t.Fatalf("remove unsafe skill name = %v", err)
	}
	if _, err := s.ReadInstalledSkill(name); err != nil {
		t.Fatalf("failed operations changed the installed package: %v", err)
	}
}

func TestConcurrentSkillInstallsPublishOneBundle(t *testing.T) {
	s := New(t.TempDir())
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		archive := makeSkillZIP(t, skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644})
		go func() {
			<-start
			_, err := s.InstallSkillZIP(t.Context(), archive)
			results <- err
		}()
	}
	close(start)
	var success, collision int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrSkillInstallConflict):
			collision++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || collision != 1 {
		t.Fatalf("concurrent installs = %d successes, %d conflicts", success, collision)
	}
	got, err := s.ListInstalledSkills()
	if err != nil || len(got) != 1 {
		t.Fatalf("concurrent installs index = %+v, %v", got, err)
	}
}

func TestFinderArchivePublishesOnlyTheChosenWrapper(t *testing.T) {
	s := New(t.TempDir())
	zip := makeSkillZIP(t,
		skillZIPEntry{"wrapper/", "", fs.ModeDir | 0o755},
		skillZIPEntry{"wrapper/SKILL.md", portableSkillFixture, 0o644},
		skillZIPEntry{"wrapper/references/notes.md", "reference", 0o644},
		skillZIPEntry{"__MACOSX/._wrapper", "metadata", 0o644},
		skillZIPEntry{".DS_Store", "metadata", 0o644},
	)
	if _, err := s.InstallSkillZIP(t.Context(), zip); err != nil {
		t.Fatal(err)
	}
	skill, err := s.ReadInstalledSkill("verify-layout")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadDir(skill.BaseDir)
	if err != nil || len(got) != 2 || got[0].Name() != "SKILL.md" || got[1].Name() != "references" {
		t.Fatalf("staged wrapper includes Finder metadata or missed contents: %v, %v", got, err)
	}
}

func TestInstalledSkillsDoNotExposeDamagedPackagesOrHiddenStaging(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.ReadInstalledSkill("unknown"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("lookup without catalog = %v", err)
	}
	if err := s.RemoveInstalledSkill("unknown"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("remove without catalog = %v", err)
	}

	archive := makeSkillZIP(t, skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644})
	const name = "verify-layout"
	if _, err := s.InstallSkillZIP(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.installedSkillsPath(), ".stage-abandoned"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(s.installedSkillDir(name), skillFile)
	if err := os.WriteFile(manifest, []byte(strings.Replace(portableSkillFixture, name, "different-name", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadInstalledSkill(name); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("read changed package identity = %v", err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadInstalledSkill(name); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read missing manifest = %v", err)
	}
	if err := os.Mkdir(manifest, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadInstalledSkill(name); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("read nonregular manifest = %v", err)
	}
	if got, err := s.ListInstalledSkills(); err != nil || len(got) != 0 {
		t.Fatalf("damaged or staged skill still appears: %+v, %v", got, err)
	}
	if err := s.RemoveInstalledSkill(name); err != nil {
		t.Fatalf("could not remove damaged package: %v", err)
	}

	if err := os.RemoveAll(s.installedSkillsPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.installedSkillsPath(), []byte("unrelated file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallSkillZIP(t.Context(), archive); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("install would replace non-directory catalog: %v", err)
	}
	if _, err := s.ListInstalledSkills(); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("non-directory catalog treated as empty: %v", err)
	}
	if _, err := s.ReadInstalledSkill(name); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("read from a non-directory catalog: %v", err)
	}
	if err := s.RemoveInstalledSkill(name); !errors.Is(err, ErrInvalidSkillBundle) {
		t.Fatalf("remove in a non-directory catalog: %v", err)
	}
}

func TestMalformedSkillResourcesNeverLeaveAPartialPackage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(*zip.File)
	}{
		{"unsupported compression", func(file *zip.File) { file.Method = 0xffff }},
		{"wrong checksum", func(file *zip.File) { file.CRC32++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(t.TempDir())
			archive := makeSkillZIP(t,
				skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644},
				skillZIPEntry{"references/notes.md", "resource", 0o644},
			)
			tc.damage(archive.File[1])
			if _, err := s.InstallSkillZIP(t.Context(), archive); !errors.Is(err, ErrInvalidSkillBundle) {
				t.Fatalf("install corrupt archive = %v", err)
			}
			if got, err := os.ReadDir(s.installedSkillsPath()); err != nil || len(got) != 0 {
				t.Fatalf("corrupt bundle left package or stage: %v, %v", got, err)
			}
		})
	}
}

type firstZIPRead struct {
	io.ReadCloser
	before func()
}

func (r *firstZIPRead) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.ReadCloser.Read(p)
}

func TestSkillInstallationStopsDuringExtraction(t *testing.T) {
	s := New(t.TempDir())
	archive := makeSkillZIP(t,
		skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644},
		skillZIPEntry{"assets/large", strings.Repeat("x", 1<<20), 0o644},
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var opened int
	archive.RegisterDecompressor(zip.Deflate, func(reader io.Reader) io.ReadCloser {
		opened++
		in := flate.NewReader(reader)
		if opened == 2 {
			return &firstZIPRead{ReadCloser: in, before: cancel}
		}
		return in
	})
	if _, err := s.InstallSkillZIP(ctx, archive); !errors.Is(err, context.Canceled) || opened != 2 {
		t.Fatalf("install canceled during resource extraction = %v, opened=%d", err, opened)
	}
	if got, err := os.ReadDir(s.installedSkillsPath()); err != nil || len(got) != 0 {
		t.Fatalf("canceled bundle left package or stage: %v, %v", got, err)
	}
}

func TestInFlightSkillExtractionCannotRecreateADeletedProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires removing a directory with open files")
	}
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := New(projectDir)
	archive := makeSkillZIP(t,
		skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644},
		skillZIPEntry{"references/nested/notes.md", "resource", 0o644},
	)
	deleted := false
	archive.RegisterDecompressor(zip.Deflate, func(reader io.Reader) io.ReadCloser {
		in := flate.NewReader(reader)
		if deleted {
			return in
		}
		return &firstZIPRead{ReadCloser: in, before: func() {
			if err := os.RemoveAll(projectDir); err != nil {
				t.Fatalf("delete project during staged write: %v", err)
			}
			deleted = true
		}}
	})
	if _, err := s.InstallSkillZIP(t.Context(), archive); err == nil || !deleted {
		t.Fatalf("install during project deletion = %v, deleted=%t", err, deleted)
	}
	if _, err := os.Stat(projectDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("in-flight extraction recreated deleted project: %v", err)
	}
}

func TestPortableMarkdownPreservesLeadingCodeBlockIndentation(t *testing.T) {
	const body = "    first code line\n    second code line"
	manifest := strings.Replace(portableSkillFixture, "Read [notes](references/notes.md) before continuing.", body, 1)
	skill, err := parseInstalledSkill([]byte(manifest))
	if err != nil || skill.Body != body {
		t.Fatalf("body = %q, %v", skill.Body, err)
	}
}

func TestInFlightSkillStageCannotPublishAfterProjectInvalidation(t *testing.T) {
	s := New(t.TempDir())
	original := makeSkillZIP(t, skillZIPEntry{"SKILL.md", portableSkillFixture, 0o644})
	if _, err := s.InstallSkillZIP(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	stale := makeSkillZIP(t,
		skillZIPEntry{"SKILL.md", strings.Replace(portableSkillFixture, "verify-layout", "stale-skill", 1), 0o644},
		skillZIPEntry{"references/read.md", "Private reference.", 0o644},
	)
	var opened int
	stale.RegisterDecompressor(zip.Deflate, func(reader io.Reader) io.ReadCloser {
		opened++
		in := flate.NewReader(reader)
		if opened == 1 {
			return &firstZIPRead{ReadCloser: in, before: s.Invalidate}
		}
		return in
	})
	if _, err := s.InstallSkillZIP(t.Context(), stale); !errors.Is(err, ErrProjectDeleted) || opened != 2 {
		t.Fatalf("staged install after invalidation = %v, opened %d files", err, opened)
	}
	if _, err := s.InstallSkillZIP(t.Context(), stale); !errors.Is(err, ErrProjectDeleted) || opened != 2 {
		t.Fatalf("invalidated store opened another install = %v, opened %d files", err, opened)
	}
	if _, err := s.ListInstalledSkills(); !errors.Is(err, ErrProjectDeleted) {
		t.Fatalf("list after invalidation = %v", err)
	}
	if _, err := s.ReadInstalledSkill("verify-layout"); !errors.Is(err, ErrProjectDeleted) {
		t.Fatalf("read after invalidation = %v", err)
	}
	if err := s.RemoveInstalledSkill("verify-layout"); !errors.Is(err, ErrProjectDeleted) {
		t.Fatalf("remove after invalidation = %v", err)
	}
	items, err := os.ReadDir(s.installedSkillsPath())
	if err != nil || len(items) != 1 || items[0].Name() != "verify-layout" {
		t.Fatalf("invalidated store changed visible packages or left staging: %v, %v", items, err)
	}
}

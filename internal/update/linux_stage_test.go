package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"debug/elf"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testELF(arch string) []byte {
	body := make([]byte, 128)
	copy(body, elf.ELFMAG)
	body[4], body[5], body[6] = byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(body[16:18], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(body[18:20], linuxMachine(arch))
	return body
}

func testLinuxArchive(t *testing.T, header *tar.Header, body []byte, extra bool, trailing []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if extra {
		if err := tarWriter.WriteHeader(&tar.Header{Name: "extra", Typeflag: tar.TypeReg, Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gzipWriter.Write(trailing); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func testLinuxExecutableHeader(body []byte) *tar.Header {
	return &tar.Header{
		Name: "zwai", Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(body)),
		Format: tar.FormatPAX, ModTime: time.Unix(0, 1234),
	}
}

func TestLinuxStageVerifiesAndPreservesExecutableBytesAndReportsRealProgress(t *testing.T) {
	const version = "2.0.1"
	const arch = "arm64"
	body := testELF(arch)
	archive := testLinuxArchive(t, testLinuxExecutableHeader(body), body, false, make([]byte, 1024))
	asset := linuxTestAsset(version, arch, archive)
	for _, known := range []bool{false, true} {
		dir := t.TempDir()
		feed := LinuxFeed{Current: "2.0.0", Arch: arch}
		feed.Fetch = func(_ context.Context, source string) (*http.Response, error) {
			switch source {
			case releasesURL + "/tags/v" + version:
				return linuxTestJSON(t, linuxTestRelease(version, asset)), nil
			case asset.URL:
				resp := linuxTestResponse(archive)
				if !known {
					resp.ContentLength = -1
				}
				return resp, nil
			default:
				t.Fatalf("unexpected request: %s", source)
				return nil, nil
			}
		}
		checked := false
		feed.verifyVersion = func(_ context.Context, exe, want string) error {
			checked = true
			if want != version || filepath.Dir(exe) != dir {
				t.Fatalf("verify %q at %q", want, exe)
			}
			return nil
		}
		var seen []int64
		path, err := feed.Stage(t.Context(), version, dir, func(downloaded, total int64) {
			if (known && total != int64(len(archive))) || (!known && total != 0) {
				t.Errorf("progress reported invalid total %d", total)
			}
			seen = append(seen, downloaded)
		})
		if err != nil || !checked || len(seen) < 2 || seen[0] != 0 || seen[len(seen)-1] != int64(len(archive)) {
			t.Fatalf("stage: %q, %v, checked = %t, progress = %v", path, err, checked, seen)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, body) {
			t.Fatalf("staged executable differs: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("staged executable mode: %+v, %v", info, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
			t.Fatalf("staging receipt kept unrelated files: %v, %v", entries, err)
		}
	}
}

func TestLinuxStageRefusesUnverifiedOrIncompatibleDownloads(t *testing.T) {
	const version = "2.0.1"
	const arch = "amd64"
	body := testELF(arch)
	header := testLinuxExecutableHeader(body)
	valid := testLinuxArchive(t, header, body, false, nil)
	wrongArch := testELF("arm64")
	wrongFormat := append([]byte(nil), body...)
	wrongFormat[5] = byte(elf.ELFDATA2MSB)
	wrongMode := *header
	wrongMode.Mode = 0o644
	badHeader := *header
	badHeader.Name = "wrong"
	linked := &tar.Header{Name: "zwai", Mode: 0o755, Typeflag: tar.TypeSymlink, Linkname: "alternate"}
	badCRC := append([]byte(nil), valid...)
	badCRC[len(badCRC)-8] ^= 0xff
	secondMember := append(append([]byte(nil), valid...), valid...)
	for _, tc := range []struct {
		name      string
		content   []byte
		needle    string
		wrongHash bool
	}{
		{name: "wrong hash", content: valid, needle: "SHA-256", wrongHash: true},
		{name: "wrong architecture", content: testLinuxArchive(t, header, wrongArch, false, nil), needle: "ELF"},
		{name: "wrong ELF encoding", content: testLinuxArchive(t, header, wrongFormat, false, nil), needle: "ELF"},
		{name: "wrong entry name", content: testLinuxArchive(t, &badHeader, body, false, nil), needle: "regular executable"},
		{name: "wrong mode", content: testLinuxArchive(t, &wrongMode, body, false, nil), needle: "regular executable"},
		{name: "symlink", content: testLinuxArchive(t, linked, nil, false, nil), needle: "regular executable"},
		{name: "second entry", content: testLinuxArchive(t, header, body, true, nil), needle: "one entry"},
		{name: "bad gzip", content: []byte("not gzip"), needle: "gzip"},
		{name: "bad tar", content: testLinuxArchive(t, &tar.Header{Name: "wrong", Mode: 0o755, Typeflag: tar.TypeReg}, nil, false, nil), needle: "regular executable"},
		{name: "gzip CRC", content: badCRC, needle: "archive"},
		{name: "another stream", content: secondMember, needle: "unexpected compressed data"},
		{name: "nonzero padding", content: testLinuxArchive(t, header, body, false, []byte{1}), needle: "trailing data"},
		{name: "too much padding", content: testLinuxArchive(t, header, body, false, make([]byte, maxLinuxTarPadding+1)), needle: "trailing data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sumBody := tc.content
			if tc.wrongHash {
				sumBody = []byte("another download")
			}
			asset := linuxTestAsset(version, arch, sumBody)
			dir := t.TempDir()
			feed := LinuxFeed{
				Current: "2.0.0", Arch: arch,
				Fetch: func(_ context.Context, source string) (*http.Response, error) {
					if source == releasesURL+"/tags/v"+version {
						return linuxTestJSON(t, linuxTestRelease(version, asset)), nil
					}
					return linuxTestResponse(tc.content), nil
				},
				verifyVersion: func(context.Context, string, string) error {
					t.Fatal("an unverified executable must never run")
					return nil
				},
			}
			if path, err := feed.Stage(t.Context(), version, dir, nil); path != "" || err == nil || !strings.Contains(err.Error(), tc.needle) {
				t.Fatalf("stage = %q, %v; expected %q", path, err, tc.needle)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed staging kept files: %v, %v", entries, err)
			}
		})
	}
}

func TestLinuxStageRefusesTruncatedTarEvenWithAMatchingDigest(t *testing.T) {
	const version = "2.0.1"
	const arch = "amd64"
	executable := testELF(arch)
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	if err := writer.WriteHeader(&tar.Header{Name: "zwai", Mode: 0o755, Size: int64(len(executable))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(executable); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		length int
	}{
		{"header", 160},
		{"executable", 512 + 64},
		{"end marker", 1024 + 24},
	} {
		t.Run(test.name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := gzip.NewWriter(&archive)
			if _, err := writer.Write(data.Bytes()[:test.length]); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			asset := linuxTestAsset(version, arch, archive.Bytes())
			feed := LinuxFeed{Current: "2.0.0", Arch: arch,
				Fetch: func(_ context.Context, source string) (*http.Response, error) {
					if source == releasesURL+"/tags/v"+version {
						return linuxTestJSON(t, linuxTestRelease(version, asset)), nil
					}
					return linuxTestResponse(archive.Bytes()), nil
				},
				verifyVersion: func(context.Context, string, string) error {
					t.Error("the updater ran an executable from a truncated archive")
					return nil
				},
			}
			dir := t.TempDir()
			if path, err := feed.Stage(t.Context(), version, dir, nil); path != "" || !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("truncated stage = %q, %v", path, err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("truncated package left staging files: %v, %v", files, err)
			}
		})
	}
}

func TestLinuxStageReportsMetadataTransportAndVersionProbeFailures(t *testing.T) {
	const version = "2.0.1"
	const arch = "arm64"
	body := testELF(arch)
	archive := testLinuxArchive(t, testLinuxExecutableHeader(body), body, false, nil)
	asset := linuxTestAsset(version, arch, archive)
	feed := LinuxFeed{Current: "2.0.0", Arch: arch}
	for _, tc := range []struct {
		name, needle string
		response     func() (*http.Response, error)
	}{
		{"bad metadata", "response", func() (*http.Response, error) { return linuxTestResponse([]byte("invalid")), nil }},
		{"wrong tag", "no verified", func() (*http.Response, error) { return linuxTestJSON(t, linuxTestRelease("2.0.2", asset)), nil }},
		{"offline", "offline", func() (*http.Response, error) { return nil, errors.New("offline") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed.Fetch = func(_ context.Context, source string) (*http.Response, error) {
				if source != releasesURL+"/tags/v"+version {
					t.Fatalf("unexpected request after bad metadata: %s", source)
				}
				return tc.response()
			}
			if _, err := feed.Stage(t.Context(), version, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), tc.needle) {
				t.Fatalf("stage error = %v, expected %q", err, tc.needle)
			}
		})
	}
	dir := t.TempDir()
	feed.Fetch = func(_ context.Context, source string) (*http.Response, error) {
		if source == releasesURL+"/tags/v"+version {
			return linuxTestJSON(t, linuxTestRelease(version, asset)), nil
		}
		return linuxTestResponse(archive), nil
	}
	if _, err := feed.Stage(t.Context(), version, filepath.Join(dir, "missing"), nil); err == nil || !strings.Contains(err.Error(), "stage Linux download") {
		t.Fatalf("missing path stage: %v", err)
	}
	feed.verifyVersion = func(context.Context, string, string) error { return errors.New("bad build version") }
	if _, err := feed.Stage(t.Context(), version, dir, nil); err == nil || !strings.Contains(err.Error(), "bad build version") {
		t.Fatalf("stage bad version: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("failed version probe kept staged files: %v, %v", entries, err)
	}
}

func TestLinuxDownloadIsBoundedCancellableAndProtectsRedirectCredentials(t *testing.T) {
	const version = "2.0.1"
	offer := pickLinuxAsset(version, "amd64", linuxTestRelease(version, linuxTestAsset(version, "amd64", []byte("short"))))
	for _, tc := range []struct {
		name, needle string
		reply        func() (*http.Response, error)
	}{
		{"too large", "too large", func() (*http.Response, error) {
			resp := linuxTestResponse(nil)
			resp.ContentLength = maxLinuxArchive + 1
			return resp, nil
		}},
		{"truncated", "size did not match", func() (*http.Response, error) {
			resp := linuxTestResponse([]byte("short"))
			resp.ContentLength++
			return resp, nil
		}},
		{"not found", "HTTP 404", func() (*http.Response, error) {
			resp := linuxTestResponse(nil)
			resp.StatusCode = http.StatusNotFound
			return resp, nil
		}},
		{"redirect failure", "timed out", func() (*http.Response, error) {
			return nil, &url.Error{Op: "Get", URL: "https://release-assets.githubusercontent.com/file?secret=test", Err: errors.New("timed out")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed := LinuxFeed{Fetch: func(context.Context, string) (*http.Response, error) { return tc.reply() }}
			err := feed.download(t.Context(), offer, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), tc.needle) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("download error = %v", err)
			}
		})
	}
	feed := LinuxFeed{Fetch: func(context.Context, string) (*http.Response, error) {
		return linuxTestResponse([]byte("short")), nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := feed.download(ctx, offer, io.Discard, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("download cancellation = %v", err)
	}
}

func TestLinuxVersionProbeUsesActualVersionOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs an executable Unix fixture")
	}
	path := filepath.Join(t.TempDir(), "zwai")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 1\nprintf 'zwai 2.0.1\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxVersion(t.Context(), path, "2.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxVersion(t.Context(), path, "2.0.2"); err == nil || !strings.Contains(err.Error(), "different version") {
		t.Fatalf("wrong version probe error = %v", err)
	}
	if err := verifyLinuxVersion(t.Context(), path+".missing", "2.0.1"); err == nil {
		t.Fatal("missing staged executable passed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := verifyLinuxVersion(ctx, path, "2.0.1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("version probe lost cancellation: %v", err)
	}
}

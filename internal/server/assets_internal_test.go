package server

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestFreezeAssetsCopiesEveryFile(t *testing.T) {
	src := fstest.MapFS{
		"index.html":        {Data: []byte("shell")},
		"assets/index-a.js": {Data: []byte("js")},
	}
	got, err := freezeAssets(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(got, "assets/index-a.js")
	if err != nil || string(data) != "js" {
		t.Fatalf("got %q err %v", data, err)
	}
	src["assets/index-a.js"] = &fstest.MapFile{Data: []byte("mutated")}
	data, err = fs.ReadFile(got, "assets/index-a.js")
	if err != nil || string(data) != "js" {
		t.Fatalf("freeze was live: %q err %v", data, err)
	}
	if _, err := got.Open("assets"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a directory listing would leak hashed names: %v", err)
	}
	if _, err := got.Open(".."); err == nil {
		t.Fatal("an unclean path opened")
	}
}

func TestFreezeAssetsSkipsAFileThatVanishesMidWalk(t *testing.T) {
	inner := fstest.MapFS{
		"index.html": {Data: []byte("shell")},
		"ghost.js":   {Data: []byte("gone")},
	}
	got, err := freezeAssets(hideFile{FS: inner, hide: "ghost.js"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(got, "index.html")
	if err != nil || string(data) != "shell" {
		t.Fatalf("got %q err %v", data, err)
	}
	if _, err := got.Open("ghost.js"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("vanished file leaked into the freeze: %v", err)
	}
}

func TestFreezeAssetsReportsAWalkError(t *testing.T) {
	if _, err := freezeAssets(errFS{}); err == nil {
		t.Fatal("expected a walk error")
	}
}

func TestFreezeAssetsTreatsAMissingTreeAsEmpty(t *testing.T) {
	got, err := freezeAssets(missingFS{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := got.Open("index.html"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty freeze: %v", err)
	}
}

func TestBundledFileIsTheHashedTreeAndAnythingWithAnExtension(t *testing.T) {
	for _, name := range []string{"assets", "assets/index-a.js", "assets/missing", "index.css", "favicon.ico"} {
		if !bundledFile(name) {
			t.Fatalf("%s must 404 rather than become the SPA shell", name)
		}
	}
	for _, name := range []string{"threads/th_abc", "settings", "t"} {
		if bundledFile(name) {
			t.Fatalf("%s is a client route and must keep the SPA fallback", name)
		}
	}
}

func TestSnapFileSeekAndRead(t *testing.T) {
	f := &snapFile{name: "assets/a.js", data: []byte("abcdef")}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != 0o444 || info.Sys() != nil {
		t.Fatalf("mode=%v sys=%v", info.Mode(), info.Sys())
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(1, io.SeekCurrent); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	n, err := f.Read(buf)
	if err != nil || n != 3 || string(buf) != "def" {
		t.Fatalf("read %d %q err %v", n, buf, err)
	}
	if _, err := f.Seek(-1, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	one := make([]byte, 1)
	if n, err = f.Read(one); err != nil || n != 1 || one[0] != 'f' {
		t.Fatalf("end read %d %q err %v", n, one, err)
	}
	if _, err := f.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek")
	}
	if _, err := f.Seek(0, 99); err == nil {
		t.Fatal("bogus whence")
	}
}

type errFS struct{}

func (errFS) Open(string) (fs.File, error) { return nil, errors.New("boom") }

type missingFS struct{}

func (missingFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

type hideFile struct {
	fs.FS
	hide string
}

func (h hideFile) Open(name string) (fs.File, error) {
	if name == h.hide {
		return nil, fs.ErrNotExist
	}
	return h.FS.Open(name)
}

package update

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseVersionIsANumericTriple(t *testing.T) {
	if Canonical("v1.2.3") != "1.2.3" || !Newer("1.2.4", "1.2.3") {
		t.Fatal("a newer triple must win")
	}
	if Newer("1.2.3", "1.2.3") || Newer("1.2.3", "1.3.0") || Newer("1.10.0", "1.9.0") == false {
		t.Fatal("compare by numbers, not text")
	}
	for _, raw := range []string{"", "dev", "1.2", "1.2.3-dirty", "v1.2.3-5-gabcdef"} {
		if Parse(raw) != nil {
			t.Fatalf("%q is not a release version", raw)
		}
	}
	if DarwinAssetName("v1.2.3", "arm64") != "zwai-1.2.3-darwin-arm64.zip" {
		t.Fatal("asset name drifted")
	}
}

func TestDownloadURLStaysOnThisRepo(t *testing.T) {
	ok := "https://github.com/" + ReleaseRepo + "/releases/download/v1.2.3/zwai-1.2.3-darwin-arm64.zip"
	if AllowedDownload(ok) == "" {
		t.Fatal("this repo's asset must be accepted")
	}
	for _, raw := range []string{
		"http://github.com/" + ReleaseRepo + "/releases/download/v1.2.3/zwai.zip",
		"https://github.com/other/repo/releases/download/v1.2.3/zwai-1.2.3-darwin-arm64.zip",
		"https://objects.githubusercontent.com/zwai-1.2.3-darwin-arm64.zip",
		"https://evil.example/releases/download/v1.2.3/zwai.zip",
	} {
		if AllowedDownload(raw) != "" {
			t.Fatalf("accepted %s", raw)
		}
	}
	if !AllowedHost("release-assets.githubusercontent.com") || AllowedHost("evil.example") {
		t.Fatal("redirect hosts drifted")
	}
}

func TestCheckIgnoresDraftsAndTheCurrentBuild(t *testing.T) {
	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64"}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.3", false, true)), nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "current" {
		t.Fatalf("prerelease: %+v", got)
	}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.2", false, false)), nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "current" {
		t.Fatalf("older: %+v", got)
	}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(`{"message":"Not Found"}`), nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "error" || got.Message != "Not Found" {
		t.Fatalf("api error: %+v", got)
	}
}

func TestCheckOffersTheMatchingMacZip(t *testing.T) {
	var hits int
	svc := &Service{
		Current: "1.2.3",
		OS:      "darwin",
		Arch:    "arm64",
		Now:     func() time.Time { return time.Unix(1000, 0) },
	}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		hits++
		return jsonResponse(releaseJSON("1.4.0", false, false)), nil
	}
	got := svc.Check(context.Background(), false)
	if got.Status != "available" || got.Offer == nil || got.Offer.AssetName != "zwai-1.4.0-darwin-arm64.zip" {
		t.Fatalf("offer: %+v", got)
	}
	if !strings.Contains(got.Offer.AssetURL, "/releases/download/") {
		t.Fatal(got.Offer.AssetURL)
	}
	again := svc.Check(context.Background(), false)
	if again.Offer == nil || hits != 1 {
		t.Fatalf("cache missed, hits=%d %+v", hits, again)
	}
	svc.Now = func() time.Time { return time.Unix(1000, 0).Add(checkTTL + time.Second) }
	_ = svc.Check(context.Background(), false)
	if hits != 2 {
		t.Fatalf("stale cache hits=%d", hits)
	}
}

func TestCheckSkipsOtherSystemsAndUntaggedBuilds(t *testing.T) {
	linux := &Service{Current: "1.2.3", OS: "linux", Arch: "amd64"}
	if got := linux.Check(context.Background(), true); got.Status != "unsupported" {
		t.Fatalf("%+v", got)
	}
	dev := &Service{Current: "dev", OS: "darwin", Arch: "arm64"}
	if got := dev.Check(context.Background(), true); got.Status != "error" {
		t.Fatalf("%+v", got)
	}
}

func TestApplySwapsTheRunningApp(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "zwai.app")
	bin := filepath.Join(app, "Contents", "MacOS", "zwai")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := bytes.NewBuffer(nil)
	if err := ZipApp(mustApp(t, "new"), filepath.Join(root, "src.zip")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "src.zip"))
	if err != nil {
		t.Fatal(err)
	}
	payload.Write(raw)
	var opened string
	var signaled []int
	var script string
	svc := &Service{
		Current: "1.2.3",
		OS:      "darwin",
		Arch:    "arm64",
		Exe:     func() (string, error) { return bin, nil },
		Open:    func(path string) error { opened = path; return nil },
		Signal:  func(pid int) { signaled = append(signaled, pid) },
		Spawn:   func(path string) error { b, _ := os.ReadFile(path); script = string(b); return nil },
	}
	svc.Fetch = func(_ context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			return jsonResponse(releaseJSON("1.2.4", false, false)), nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       ioNopCloserBytes(bytes.NewReader(payload.Bytes())),
			Header:     make(http.Header),
		}, nil
	}
	if err := svc.Apply(context.Background(), "v1.2.4", []int{42, os.Getpid(), 42}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("binary %q", got)
	}
	if opened != app {
		t.Fatalf("opened %s", opened)
	}
	if len(signaled) != 1 || signaled[0] != 42 {
		t.Fatalf("signals %v", signaled)
	}
	if !strings.Contains(script, "zwai.app.previous") {
		t.Fatalf("cleanup script %s", script)
	}
}

func TestApplyRejectsALooseBinaryAndABadZip(t *testing.T) {
	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64", Exe: func() (string, error) {
		return "/tmp/zwai", nil
	}}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.4", false, false)), nil
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil || !strings.Contains(err.Error(), "zwai.app") {
		t.Fatal(err)
	}
	if _, err := InstalledApp("/tmp/Other.app/Contents/MacOS/other"); err == nil {
		t.Fatal("a different executable is not this app")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.zip")
	writeZip(t, bad, map[string]string{"../zwai.app/Contents/MacOS/zwai": "x"})
	if err := unzipApp(bad, filepath.Join(dir, "out")); err == nil {
		t.Fatal("zip slip must fail")
	}
}

func mustApp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "zwai.app")
	bin := filepath.Join(app, "Contents", "MacOS", "zwai")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return app
}

func releaseJSON(version string, draft, pre bool) string {
	name := DarwinAssetName(version, "arm64")
	page := "https://github.com/" + ReleaseRepo + "/releases/tag/v" + version
	asset := "https://github.com/" + ReleaseRepo + "/releases/download/v" + version + "/" + name
	flag := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	return `{"tag_name":"v` + version + `","html_url":"` + page + `","draft":` + flag(draft) +
		`,"prerelease":` + flag(pre) + `,"assets":[{"name":"` + name + `","browser_download_url":"` + asset + `"},{"name":"zwai-` + version + `-android.apk","browser_download_url":"` + asset + `.apk"}]}`
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       ioNopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }

func ioNopCloser(r *strings.Reader) nopCloser { return nopCloser{r} }

type bytesCloser struct{ *bytes.Reader }

func (bytesCloser) Close() error { return nil }

func ioNopCloserBytes(r *bytes.Reader) bytesCloser { return bytesCloser{r} }

func TestHostArchAndFeedErrors(t *testing.T) {
	if archFor("linux", "amd64") != "" || archFor("darwin", "386") != "" {
		t.Fatal("only darwin arm64 and amd64 install")
	}
	if archFor("darwin", "arm64") != "arm64" || system("linux", "amd64") != "other" || system("darwin", "amd64") != "darwin" {
		t.Fatal("system class drifted")
	}
	if HostArch() == "" && hostOS() == "darwin" {
		t.Fatal("host os and arch disagree")
	}
	_ = HostArch()
	_ = hostOS()

	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "amd64"}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("2.0.0", false, false)), nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "error" || !strings.Contains(got.Message, "no macOS") {
		t.Fatalf("wrong arch: %+v", got)
	}
	svc.Arch = "arm64"
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse("{"), nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "error" {
		t.Fatalf("bad json: %+v", got)
	}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       ioNopCloser(strings.NewReader(`{"message":"rate limit"}`)),
			Header:     make(http.Header),
		}, nil
	}
	if got := svc.Check(context.Background(), true); got.Status != "error" || got.Message != "rate limit" {
		t.Fatalf("status: %+v", got)
	}
	if AllowedPage("https://github.com/"+ReleaseRepo+"/releases/tag/v1.2.3") == "" {
		t.Fatal("release page must be allowed")
	}
	if AllowedPage("https://example.com/releases/tag/v1") != "" || AllowedPage("https://user:pw@github.com/"+ReleaseRepo+"/releases/tag/v1") != "" {
		t.Fatal("other hosts are not the release page")
	}
	none := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64"}
	none.Fetch = func(context.Context, string) (*http.Response, error) {
		return nil, os.ErrClosed
	}
	if got := none.Check(context.Background(), true); got.Status != "error" {
		t.Fatalf("feed down: %+v", got)
	}
	none.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(`{"tag_name":"v9.0.0","html_url":"https://evil.example/nope","assets":[{"name":"zwai-9.0.0-darwin-arm64.zip","browser_download_url":"https://evil.example/nope.zip"}]}`), nil
	}
	if got := none.Check(context.Background(), true); got.Status != "error" {
		t.Fatalf("foreign asset: %+v", got)
	}
	none.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(`{"tag_name":"latest"}`), nil
	}
	if got := none.Check(context.Background(), true); got.Status != "error" || got.Message == "" {
		t.Fatalf("untagged: %+v", got)
	}
	none.Fetch = func(context.Context, string) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Status: "500 broken", Body: ioNopCloser(strings.NewReader("plain")), Header: make(http.Header)}, nil
	}
	if got := none.Check(context.Background(), true); got.Message != "500 broken" {
		t.Fatalf("status line: %+v", got)
	}
	if AllowedPage("https://github.com/"+ReleaseRepo+"/blob/main/README.md") != "" {
		t.Fatal("a source page is not a release")
	}
	app := filepath.Join(t.TempDir(), "zwai.app")
	if err := os.MkdirAll(app+".previous", 0o755); err != nil {
		t.Fatal(err)
	}
	(&Service{}).retirePrevious(app)
	exe, err := (&Service{}).executable()
	if err != nil || exe == "" {
		t.Fatal(err)
	}
}

func TestDefaultFetchRejectsARedirectOffGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "https://evil.example/payload", http.StatusFound)
	}))
	defer srv.Close()
	svc := &Service{}
	res, err := svc.fetch(context.Background(), srv.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if _, err := svc.fetch(context.Background(), srv.URL+"/away"); err == nil {
		t.Fatal("a redirect off GitHub must fail")
	}
	script := filepath.Join(t.TempDir(), "gone.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := spawnDetached(script); err != nil {
		t.Fatal(err)
	}
	signalPID(1<<30 + 7)
	(&Service{}).retirePrevious(filepath.Join(t.TempDir(), "missing.app"))
}

func TestDownloadAndUnzipRejectGarbage(t *testing.T) {
	svc := &Service{}
	if err := svc.download(context.Background(), "https://evil.example/a.zip", filepath.Join(t.TempDir(), "a.zip")); err == nil {
		t.Fatal("foreign url")
	}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       ioNopCloser(strings.NewReader("missing")),
			Header:     make(http.Header),
		}, nil
	}
	asset := "https://github.com/" + ReleaseRepo + "/releases/download/v1.2.4/zwai-1.2.4-darwin-arm64.zip"
	if err := svc.download(context.Background(), asset, filepath.Join(t.TempDir(), "a.zip")); err == nil {
		t.Fatal("404")
	}
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.zip")
	writeZip(t, empty, map[string]string{"zwai.app/Contents/Info.plist": "no bin"})
	if err := unzipApp(empty, filepath.Join(dir, "out")); err == nil {
		t.Fatal("an archive without the executable must fail")
	}
	if err := unzipApp(filepath.Join(dir, "nope.zip"), dir); err == nil {
		t.Fatal("not a zip")
	}
	if err := ZipApp(filepath.Join(dir, "missing.app"), filepath.Join(dir, "out.zip")); err == nil {
		t.Fatal("missing app dir")
	}
}

func TestApplyStopsAtTheFirstFailure(t *testing.T) {
	blocked := &Service{Current: "1.2.3", OS: "linux", Arch: "amd64"}
	if err := blocked.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("linux has no desktop package")
	}
	root := t.TempDir()
	parent := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(parent, "zwai.app", "Contents", "MacOS", "zwai")
	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64", Exe: func() (string, error) {
		return "", os.ErrNotExist
	}}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.4", false, false)), nil
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("missing executable")
	}
	svc.Exe = func() (string, error) { return exe, nil }
	svc.Fetch = func(_ context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			return jsonResponse(releaseJSON("1.2.4", false, false)), nil
		}
		return nil, os.ErrClosed
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("download")
	}
	svc.Fetch = func(_ context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			return jsonResponse(releaseJSON("1.2.4", false, false)), nil
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: ioNopCloser(strings.NewReader("nope")), Header: make(http.Header)}, nil
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("bad zip")
	}
	good := bytes.NewBuffer(nil)
	if err := ZipApp(mustApp(t, "n"), filepath.Join(root, "ok.zip")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "ok.zip"))
	if err != nil {
		t.Fatal(err)
	}
	good.Write(raw)
	svc.Fetch = func(_ context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			return jsonResponse(releaseJSON("1.2.4", false, false)), nil
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: ioNopCloserBytes(bytes.NewReader(good.Bytes())), Header: make(http.Header)}, nil
	}
	svc.Exe = func() (string, error) { return exe, nil }
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("swap")
	}
	live := mustApp(t, "old")
	svc.Exe = func() (string, error) {
		return filepath.Join(live, "Contents", "MacOS", "zwai"), nil
	}
	svc.Open = func(string) error { return os.ErrPermission }
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("open")
	}
}

func TestApplyDoesNothingWhenTheFeedIsNotNewer(t *testing.T) {
	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64"}
	svc.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.3", false, false)), nil
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err == nil {
		t.Fatal("a matching offer is required")
	}
	if err := svc.Apply(context.Background(), "nope", nil); err == nil {
		t.Fatal("version required")
	}
}

func TestSwapAndSignalEdgeCases(t *testing.T) {
	if Newer("", "1.2.3") || Newer("1.2.3", "") {
		t.Fatal("unparsed versions are not newer")
	}
	src := mustApp(t, "body")
	if err := swapApp(src, filepath.Join(t.TempDir(), "missing-parent", "zwai.app")); err == nil {
		t.Fatal("renaming onto a missing bundle must fail")
	}
	linked := mustApp(t, "body")
	if err := os.Symlink("zwai", filepath.Join(linked, "Contents", "MacOS", "alias")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "zwai.app")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := swapApp(linked, dest); err == nil {
		t.Fatal("a symlink inside the app must not be installed")
	}
	previous := filepath.Join(t.TempDir(), "zwai.app.previous")
	if err := os.MkdirAll(previous, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Spawn: func(string) error { return os.ErrPermission }}
	svc.retirePrevious(strings.TrimSuffix(previous, ".previous"))
	(&Service{}).signal([]int{0, -1, 1<<30 + 11})
	blank := &Service{Current: "1.2.3"}
	blank.Fetch = func(context.Context, string) (*http.Response, error) {
		return jsonResponse(releaseJSON("1.2.3", true, false)), nil
	}
	if got := blank.Check(context.Background(), true); got.Status != "current" && got.Status != "unsupported" {
		t.Fatalf("%+v", got)
	}
}

func TestOpenUsesThePlatformLauncher(t *testing.T) {
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip()
	}
	// open is macOS. The default path must still be a real exec, so point
	// PATH at a stub named open.
	dir := t.TempDir()
	stub := filepath.Join(dir, "open")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := (&Service{}).open(filepath.Join(dir, "zwai.app")); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, dest string, files map[string]string) {
	t.Helper()
	out, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
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

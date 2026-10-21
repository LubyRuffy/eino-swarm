package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The updater must expose bytes from the actual body while the synchronous
// install request remains pending; text saying “downloading” is not progress.
func TestApplyReportsBodyProgressAndRetainsFailureForRetry(t *testing.T) {
	svc, bin := recoveryService(t)
	if got := svc.Progress(); got.Stage != "idle" || got.DownloadedBytes != 0 || got.TotalBytes != 0 {
		t.Fatalf("initial progress: %+v", got)
	}
	fetch := svc.Fetch
	reader, writer := io.Pipe()
	payloadErr := errors.New("the connection stopped before the update arrived")
	started := make(chan struct{})
	var checking Progress
	svc.Fetch = func(ctx context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			checking = svc.Progress()
			return fetch(ctx, rawURL)
		}
		close(started)
		return &http.Response{StatusCode: http.StatusOK, Body: reader, ContentLength: 100}, nil
	}
	finished := make(chan error, 1)
	go func() { finished <- svc.Apply(context.Background(), "v1.2.4", nil) }()
	<-started
	if _, err := writer.Write([]byte("1234567890")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for svc.Progress().DownloadedBytes != 10 {
		select {
		case <-deadline:
			t.Fatal("body bytes were never published")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	got := svc.Progress()
	if got.Stage != "downloading" || got.Version != "1.2.4" || got.DownloadedBytes != 10 || got.TotalBytes != 100 {
		t.Fatalf("body progress: %+v", got)
	}
	if checking.Stage != "checking" || checking.Version != "1.2.4" {
		t.Fatalf("checking progress: %+v", checking)
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel install: %v", err)
	}
	if got := svc.Progress(); got.Stage != "downloading" || got.DownloadedBytes != 10 {
		t.Fatalf("busy request erased progress: %+v", got)
	}
	_ = writer.CloseWithError(payloadErr)
	if err := <-finished; !errors.Is(err, payloadErr) {
		t.Fatalf("failed body: %v", err)
	}
	got = svc.Progress()
	if got.Stage != "error" || got.Error != payloadErr.Error() || got.DownloadedBytes != 10 {
		t.Fatalf("retained error: %+v", got)
	}
	assertInstalledBytes(t, bin, "old")
	svc.Fetch = func(ctx context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			if got := svc.Progress(); got.Stage != "checking" || got.DownloadedBytes != 0 || got.TotalBytes != 0 || got.Error != "" {
				t.Errorf("retry did not start clean: %+v", got)
			}
		}
		return fetch(ctx, rawURL)
	}
	svc.Open = func(string) error {
		got := svc.Progress()
		if got.Stage != "installing" || got.DownloadedBytes == 0 || got.TotalBytes != 0 {
			t.Errorf("launcher progress: %+v", got)
		}
		return nil
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := svc.Progress(); got.Stage != "restarting" || got.Error != "" {
		t.Fatalf("successful retry: %+v", got)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestInvalidVersionDoesNotReplaceTheLastProgress(t *testing.T) {
	svc := &Service{}
	if err := svc.Apply(context.Background(), "dev", nil); err == nil {
		t.Fatal("invalid version accepted")
	}
	if got := svc.Progress(); got.Stage != "idle" {
		t.Fatalf("invalid input changed state: %+v", got)
	}
}

func TestDownloadReportsUnknownLengthAndOnlyReceivedBytes(t *testing.T) {
	svc := &Service{Fetch: func(context.Context, string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: io.NopCloser(bytes.NewReader([]byte("body")))}, nil
	}}
	asset := "https://github.com/" + ReleaseRepo + "/releases/download/v1.2.4/zwai-1.2.4-darwin-arm64.zip"
	if err := svc.download(context.Background(), asset, t.TempDir()+"/body.zip"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Progress(); got.DownloadedBytes != 4 || got.TotalBytes != 0 {
		t.Fatalf("unknown total: %+v", got)
	}
}

func TestDownloadClientAllowsSlowBodiesButBoundsAndCancelsRequests(t *testing.T) {
	checkClient := defaultHTTPClient(false)
	downloadClient := defaultHTTPClient(true)
	t.Cleanup(checkClient.CloseIdleConnections)
	t.Cleanup(downloadClient.CloseIdleConnections)
	if checkClient.Timeout != 30*time.Second || downloadClient.Timeout != 30*time.Minute {
		t.Fatalf("metadata=%v download=%v", checkClient.Timeout, downloadClient.Timeout)
	}
	if transport, ok := downloadClient.Transport.(*http.Transport); !ok || transport.ResponseHeaderTimeout != 30*time.Second {
		t.Fatal("download headers lost their separate deadline")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, "https://evil.example/update.zip", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(80 * time.Millisecond):
			_, _ = w.Write([]byte("body"))
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	res, err := downloadClient.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || string(raw) != "body" {
		t.Fatalf("slow body=%q err=%v", raw, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	res, err = downloadClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	_, err = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("body ignored cancellation: %v", err)
	}
	// Exercise the same client's deadline without waiting thirty minutes.
	downloadClient.Timeout = 20 * time.Millisecond
	res, err = downloadClient.Get(srv.URL)
	if err == nil {
		_, err = io.ReadAll(res.Body)
		_ = res.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body was not bounded: %v", err)
	}
	if _, err := checkClient.Get(srv.URL + "/away"); err == nil {
		t.Fatal("download redirect escaped GitHub")
	}
}

func TestUpdateFetchRejectsUnreadableURLsAndAllowsGitHubAssetRedirects(t *testing.T) {
	if _, err := (&Service{}).fetch(context.Background(), "://unreadable"); err == nil {
		t.Fatal("an invalid update URL reached the transport")
	}
	client := defaultHTTPClient(true)
	req, err := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/update.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err != nil {
		t.Fatalf("the release asset redirect was rejected: %v", err)
	}
}

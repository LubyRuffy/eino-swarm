package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// TestNOSECGatewayFixture is an opt-in, five-minute cross-project fixture, not
// native acceptance. It serves the real client REST and embedded UI against an
// explicitly supplied loopback Gateway. OpenURL records a public verification
// link and never launches a process or touches the user's application data.
func TestNOSECGatewayFixture(t *testing.T) {
	metadataPath := os.Getenv("ZWAI_NOSEC_GATEWAY_FIXTURE")
	gateway := os.Getenv("ZWAI_NOSEC_GATEWAY_ORIGIN")
	if metadataPath == "" && gateway == "" {
		t.Skip("set both private fixture metadata path and loopback Gateway origin")
	}
	if metadataPath == "" || gateway == "" {
		t.Fatal("both fixture environment variables are required")
	}
	origin, err := fixtureLoopbackOrigin(gateway)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixturePrivateTarget(metadataPath); err != nil {
		t.Fatal(err)
	}
	assets := frontend.Assets()
	if err := fixtureCurrentAssets(assets); err != nil {
		t.Fatalf("frontend not ready; build and recompile this fixture first: %v", err)
	}
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(next *config.Config) error {
		next.Remote.Enabled = true
		next.Remote.AuthMode = config.RemoteAuthNOSEC
		next.Remote.HubURL = origin
		next.Remote.KeepAwake = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, st, provider.NewMock(cfg), log)
	host := remote.New(eng, cfg, log)
	host.SetVersion("nosec-gateway-fixture")
	metadata := fixtureMetadata{
		State: "preparing", GatewayOrigin: origin, StopPath: metadataPath + ".stop",
		ExpiresAt: time.Now().Add(5 * time.Minute).UTC(),
		Routes:    []string{"POST /api/remote/login", "DELETE /api/remote/login", "GET /api/remote/status", "POST /api/remote/offer", "GET /api/remote/bindings"},
	}
	var metadataMu sync.Mutex
	var httpServer *http.Server
	var serveDone chan error
	defer func() {
		if httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = httpServer.Shutdown(ctx)
			cancel()
			_ = httpServer.Close()
			if serveDone != nil {
				if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
					t.Errorf("fixture HTTP shutdown failed: %v", err)
				}
			}
		}
		host.Stop()
		eng.Shutdown()
		if err := st.Close(); err != nil {
			t.Errorf("fixture store close: %v", err)
		}
		metadataMu.Lock()
		defer metadataMu.Unlock()
		metadata.State = "stopped"
		if err := fixtureWriteMetadata(metadataPath, metadata); err != nil {
			t.Errorf("fixture cleanup metadata: %v", err)
		}
	}()
	srv, err := New(Options{
		Engine: eng, Logger: log, Assets: assets, Mode: ModeWeb, Version: "nosec-gateway-fixture",
		OpenURL: func(raw string) error {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme+"://"+u.Host != origin || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Path != "/admin/api/v1/auth/sso/nosec/start" || len(u.Query()) != 1 || len(u.Query()["desktop_code"]) != 1 || u.Query().Get("desktop_code") == "" {
				return errors.New("fixture refused unsafe public verification URL")
			}
			metadataMu.Lock()
			defer metadataMu.Unlock()
			metadata.VerificationURI = u.String()
			return fixtureWriteMetadata(metadataPath, metadata)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetRemote(host)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer = &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	serveDone = make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	metadataMu.Lock()
	metadata.Origin = "http://" + listener.Addr().String()
	metadata.State = "ready"
	err = fixtureWriteMetadata(metadataPath, metadata)
	metadataMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Log("loopback fixture ready; public metadata:", metadataPath)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(time.Until(metadata.ExpiresAt))
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			return
		case <-tick.C:
			if _, err := os.Lstat(metadata.StopPath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatal("fixture stop marker inspection failed")
			}
		}
	}
}

type fixtureMetadata struct {
	State           string    `json:"state"`
	Origin          string    `json:"origin"`
	GatewayOrigin   string    `json:"gateway_origin"`
	VerificationURI string    `json:"verification_uri,omitempty"`
	StopPath        string    `json:"stop_path"`
	ExpiresAt       time.Time `json:"expires_at"`
	Routes          []string  `json:"routes"`
}

func fixtureLoopbackOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("fixture requires a loopback Gateway origin without path, query or credentials")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return "", errors.New("fixture Gateway must use an explicit loopback IP and port")
	}
	return config.RemoteOrigin(raw)
}

func fixturePrivateTarget(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("fixture metadata must be an absolute clean path")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return errors.New("fixture metadata directory unavailable")
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("fixture metadata parent must be a private 0700 directory")
	}
	for _, target := range []string{path, path + ".stop"} {
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return errors.New("fixture requires fresh metadata and stop paths")
		}
	}
	return nil
}

func fixtureWriteMetadata(path string, metadata fixtureMetadata) error {
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nosec-fixture-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Match ensure_stamp.go's input selection without calling Ensure: a fixture
// must never install packages or rebuild a bundle another test is serving.
func fixtureCurrentAssets(assets fs.FS) error {
	if assets == nil {
		return errors.New("no embedded bundle")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("fixture source location unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "frontend")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "e2e" || name == "test-results" || name == "playwright-report" || name == "blob-report" || name == "coverage") {
				return filepath.SkipDir
			}
			return nil
		}
		if name == ".DS_Store" || strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".tsbuildinfo") || strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") || strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".spec.tsx") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return errors.New("frontend sources unavailable")
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
	}
	want := hex.EncodeToString(h.Sum(nil))
	stamp, err := fs.ReadFile(assets, ".source-stamp")
	if err != nil || strings.TrimSpace(string(stamp)) != want {
		return errors.New("embedded bundle source stamp is missing or stale")
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return err
	}
	diskIndex, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil || !bytes.Equal(index, diskIndex) {
		return errors.New("embedded bundle differs from the current frontend build")
	}
	return nil
}

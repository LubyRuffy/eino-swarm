package app

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

func TestHostUpdateKeepsTheActualWildcardBindAddress(t *testing.T) {
	a, err := New(Options{DataDir: t.TempDir(), Addr: "0.0.0.0:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	shutdown := sync.OnceFunc(func() { a.Shutdown(t.Context()) })
	t.Cleanup(shutdown)
	clientURL, err := a.Listen()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- a.Serve() }()
	target, err := url.Parse(clientURL)
	if err != nil {
		t.Fatal(err)
	}
	bindAddr := a.listener.Addr().String()
	bindHost, bindPort, err := net.SplitHostPort(bindAddr)
	if err != nil || !net.ParseIP(bindHost).IsUnspecified() || bindPort == "0" ||
		bindPort != target.Port() || !net.ParseIP(target.Hostname()).IsLoopback() {
		t.Fatalf("replacement bind must retain wildcard and allocated port: bind=%q client=%q err=%v", bindAddr, clientURL, err)
	}
	a.ConfigureHostUpdate(t.Context(), "1.0.0", update.HostUpdateOptions{Eligible: func() bool { return true }})
	if !a.hostUpdate.Supported() {
		t.Fatal("portable updater was not configured after the listener bound")
	}
	done := make(chan struct{})
	go func() { shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join the host updater")
	}
	if a.hostUpdate.Supported() {
		t.Fatal("host accepted an update after application shutdown")
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestCandidateUsesTheInheritedListenerAndRefusesWorkUntilPromotion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	a, err := New(Options{
		DataDir: t.TempDir(), Mock: true, NoAssets: true, DeferRestore: true,
		HostUpdateTrial: true, HostListener: listener,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Shutdown(t.Context()) })
	raw, err := a.Listen()
	if err != nil || raw != "http://"+listener.Addr().String() {
		t.Fatalf("inherited socket: %q, %v", raw, err)
	}
	served := make(chan error, 1)
	go func() { served <- a.Serve() }()
	resp, err := http.Get(raw + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("candidate identity probe was blocked with HTTP %d", resp.StatusCode)
	}
	resp, err = http.Post(raw+"/api/settings", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("candidate write reached the application with HTTP %d", resp.StatusCode)
	}
	a.EndHostUpdateTrial()
	resp, err = http.Get(raw + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promoted read failed with HTTP %d", resp.StatusCode)
	}
	a.Shutdown(t.Context())
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

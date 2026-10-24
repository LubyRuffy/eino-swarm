package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// A listener must be available before recovery, while the scheduler and new
// work remain gated; a client can attach and see the durable history meanwhile.
func TestDeferredRestoreListensBeforeTasksAreRecovered(t *testing.T) {
	a, err := New(Options{DataDir: t.TempDir(), Addr: "127.0.0.1:0", Mock: true, NoAssets: true, DeferRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Shutdown(context.Background()) })
	url, err := a.Listen()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- a.Serve() }()
	t.Cleanup(func() {
		a.Shutdown(context.Background())
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	resp, err := http.Get(url + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Startup string `json:"startup"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if meta.Startup != "restoring" || a.Engine.ActiveWork() == 0 {
		t.Fatalf("not protected during recovery: %+v", meta)
	}
	if err := a.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(url + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.Startup != "ready" {
		t.Fatalf("not ready after recovery: %+v", meta)
	}
}

func TestCancelledRestoreStaysRecoverableAndShutdownCancelsItsOwner(t *testing.T) {
	a, err := New(Options{DataDir: t.TempDir(), Mock: true, NoAssets: true, DeferRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Restore(ctx); err == nil {
		t.Fatal("cancelled restoration accepted")
	}
	if state, _ := a.Engine.StartupStatus(); state != "restoring" {
		t.Fatal("cancellation opened admission")
	}
	if err := a.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.Shutdown(context.Background())
	b, err := New(Options{DataDir: t.TempDir(), Mock: true, NoAssets: true, DeferRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	b.Shutdown(context.Background())
	if err := b.Restore(context.Background()); err == nil {
		t.Fatal("shutdown revived restoration")
	}
}

//go:build darwin

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

func TestRecoveryReporterShutdownDrainsAcceptedRowsWithoutAnotherTraceRequest(t *testing.T) {
	started := make(chan struct{})
	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if requests.Add(1) == 1 {
			close(started)
		}
		<-r.Context().Done()
	}))
	defer ts.Close()
	spec := desktopRecoverySpec{SessionID: "owned", Engine: lease.Record{PID: os.Getpid(), URL: ts.URL}, Journal: filepath.Join(t.TempDir(), "recovery.jsonl")}
	report, finish := desktopRecoveryReporter(spec)
	defer finish()
	report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "probe_failed", Reason: "in flight"})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first Trace request never reached the HTTP adapter")
	}
	for i := 0; i < 32; i++ {
		report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "probe_failed"})
	}
	var producers sync.WaitGroup
	begin := make(chan struct{})
	for i := 0; i < 8; i++ {
		producers.Add(1)
		go func() {
			defer producers.Done()
			<-begin
			report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "ready"})
		}()
	}
	close(begin)
	before := time.Now()
	finish()
	producers.Wait()
	if time.Since(before) > 2*time.Second || requests.Load() != 1 {
		t.Fatalf("shutdown opened another Trace request or delayed recovery: requests=%d elapsed=%v", requests.Load(), time.Since(before))
	}
	body, err := os.ReadFile(spec.Journal)
	if err != nil || len(bytes.Split(bytes.TrimSpace(body), []byte{'\n'})) != 33 {
		t.Fatalf("shutdown lost accepted local diagnostics: %q %v", body, err)
	}
	for _, row := range bytes.Split(bytes.TrimSpace(body), []byte{'\n'}) {
		var event desktop.RecoveryDiagnostic
		if json.Unmarshal(row, &event) != nil || event.SessionID != spec.SessionID || event.EnginePID != spec.Engine.PID || event.Phase != "probe_failed" {
			t.Fatalf("shutdown changed diagnostic attribution or accepted a late row: %s", row)
		}
	}
	report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "ready"})
	if after, err := os.ReadFile(spec.Journal); err != nil || !bytes.Equal(after, body) {
		t.Fatal("a producer wrote after reporter shutdown")
	}
}

func TestRecoveryReporterLongASCIICJKReasonsReachTheActualTraceHTTPAdapter(t *testing.T) {
	a, err := app.New(app.Options{DataDir: t.TempDir(), Mock: true, Mode: server.ModeEngine, Version: "fixture", NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if a.Search != nil {
			a.Search.Stop()
		}
		a.Engine.Shutdown()
		_ = a.Store.Close()
	}()
	type receipt struct {
		status int
		event  desktop.RecoveryDiagnostic
	}
	ack := make(chan receipt, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var event desktop.RecoveryDiagnostic
		_ = json.Unmarshal(body, &event)
		recorder := httptest.NewRecorder()
		a.Server.Handler().ServeHTTP(recorder, r)
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
		ack <- receipt{status: recorder.Code, event: event}
	}))
	defer ts.Close()
	spec := desktopRecoverySpec{SessionID: "session", Engine: lease.Record{PID: os.Getpid(), URL: ts.URL}, Journal: filepath.Join(t.TempDir(), "recovery.jsonl")}
	report, finish := desktopRecoveryReporter(spec)
	defer finish()
	for _, reason := range []string{strings.Repeat("ASCII failure ", 80), strings.Repeat("恢复界面错误", 80)} {
		report(desktop.RecoveryDiagnostic{SessionID: "session", Generation: 1, Attempt: 1, ShellPID: os.Getpid(), Phase: "replacement_failed", Reason: reason, At: time.Now()})
		select {
		case receipt := <-ack:
			if receipt.status != http.StatusOK || len(receipt.event.Reason) > 256 || !utf8.ValidString(receipt.event.Reason) {
				t.Fatalf("actual Trace HTTP adapter rejected normalized reason: status=%d bytes=%d valid=%v", receipt.status, len(receipt.event.Reason), utf8.ValidString(receipt.event.Reason))
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Trace request acknowledgement missing")
		}
	}
}

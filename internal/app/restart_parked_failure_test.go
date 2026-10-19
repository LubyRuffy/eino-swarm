package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Startup must expose a failed parked recovery without inventing execution,
// losing its durable checkpoint, or taking ownership of the next manager turn.
func TestNewReportsParkedRecoveryFailureWithoutClaimingWorkResumed(t *testing.T) {
	for _, test := range []struct {
		name, providerID, reason string
		timeoutFailures          int
	}{
		{name: "removed provider", providerID: "removed-provider", reason: "unknown provider"},
		{name: "exhausted watchdog", reason: "automatic recovery is exhausted", timeoutFailures: swarm.MaxConsecutiveWorkerTimeouts},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
				t.Setenv(name, "")
			}
			dir := t.TempDir()
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
			cfg.Swarm.AutoTitle = false
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(cfg.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			th := &store.Thread{Title: "parked recovery", ProviderID: cfg.Models.Default}
			if err := st.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: th.ID, ProviderID: th.ProviderID, UserText: "continue assigned work"}
			if test.providerID != "" {
				turn.ProviderID = test.providerID
			}
			if err := st.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := st.FinishTurn(turn.ID, store.TurnDone, "waiting for the next check", ""); err != nil {
				t.Fatal(err)
			}
			wake := &store.Schedule{Kind: store.ScheduleThread, ThreadID: th.ID, Prompt: "check progress",
				EveryS: 3600, NextRunAt: time.Now().UTC().Add(time.Hour), CreatedBy: store.ScheduleCreatedManager}
			if err := st.CreateSchedule(wake); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{"version": 1, "workers": []swarm.WorkerCheckpoint{{
				ID: "worker-7", Role: "verifier", Instruction: "Inspect assigned work.", Task: "continue assigned work",
				History: []*schema.Message{schema.UserMessage("continue assigned work")}, TimeoutFailures: test.timeoutFailures,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: engine.KindRestartCheckpoint,
				AgentID: swarm.DefaultManagerID, Text: string(body)}); err != nil {
				t.Fatal(err)
			}
			before, err := st.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}

			// A real file keeps concurrent log writes safe without a test-only
			// logger hook or a pipe that startup could fill and block on.
			logFile, err := os.CreateTemp(t.TempDir(), "startup-*.log")
			if err != nil {
				t.Fatal(err)
			}
			previousStderr := os.Stderr
			os.Stderr = logFile
			t.Cleanup(func() {
				os.Stderr = previousStderr
				_ = logFile.Close()
			})
			a, err := New(Options{DataDir: dir, Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
			if err != nil {
				t.Fatalf("a parked-worker failure prevented startup: %v", err)
			}
			t.Cleanup(func() { a.Shutdown(context.Background()) })
			logs, err := os.ReadFile(logFile.Name())
			if err != nil {
				t.Fatal(err)
			}
			var warning string
			for _, line := range strings.Split(string(logs), "\n") {
				if strings.Contains(line, `msg="could not resume parked workers from a previous run"`) {
					warning = line
				}
			}
			for _, detail := range []string{"level=WARN", "err=", th.ID, test.reason} {
				if !strings.Contains(warning, detail) {
					t.Fatalf("startup did not expose %q in its recovery warning: %s", detail, logs)
				}
			}
			if strings.Contains(string(logs), `msg="resumed parked workers from a previous run"`) {
				t.Fatalf("failed recovery claimed workers resumed: %s", logs)
			}
			if status := a.Engine.Status(th.ID); status.Running || status.Workers != 0 || !status.Waiting || a.Engine.ActiveWork() != 0 {
				t.Fatalf("failed recovery claimed live work or lost the future wake: %+v", status)
			}
			if turns, err := a.Store.ListTurns(th.ID); err != nil || len(turns) != 1 || turns[0].Status != store.TurnDone || turns[0].Final != "waiting for the next check" || turns[0].Error != "" {
				t.Fatalf("failed worker recovery changed the manager lifecycle: %+v, %v", turns, err)
			}
			if after, err := a.Store.GetSchedule(wake.ID); err != nil || !reflect.DeepEqual(after, wake) {
				t.Fatalf("failed recovery changed the future wake: before=%+v after=%+v error=%v", wake, after, err)
			}
			if runs, err := a.Store.ListRuns(wake.ID); err != nil || len(runs) != 0 {
				t.Fatalf("failed recovery fired the future wake: %+v, %v", runs, err)
			}
			if events, err := a.Store.ListTurnEvents(turn.ID); err != nil || !reflect.DeepEqual(events, before) {
				t.Fatalf("failed recovery fabricated lifecycle events or lost its checkpoint: %+v, %v", events, err)
			}
			if calls, err := a.Store.ListLLMCalls(turn.ID); err != nil || len(calls) != 0 {
				t.Fatalf("failed recovery executed a model call: %+v, %v", calls, err)
			}
			if _, err := os.Stat(filepath.Join(a.Engine.WorkspaceDir(th.ID), "notes", "verifier.md")); !os.IsNotExist(err) {
				t.Fatalf("failed recovery produced worker output: %v", err)
			}
			response := httptest.NewRecorder()
			a.Server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/threads/"+th.ID, nil))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), th.ID) {
				t.Fatalf("startup left the saved conversation inaccessible: %d %s", response.Code, response.Body.String())
			}
			a.Shutdown(context.Background())
			if _, err := a.Store.ListTurns(th.ID); err == nil {
				t.Fatal("shutdown left the recovered application's database open")
			}
		})
	}
}

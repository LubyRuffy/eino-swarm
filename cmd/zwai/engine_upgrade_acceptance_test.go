//go:build !windows

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// This opt-in gate uses actual distinct historical and candidate executables.
// Both engines own only the harness's fresh data/workspace and loopback model;
// passing binary paths never authorizes opening a user's data or native window.
func TestUpgradeOlderEnginePreservesPrivateWorkAndHumanPause(t *testing.T) {
	oldBinary, newBinary := os.Getenv("ZWAI_UPGRADE_OLD_BINARY"), os.Getenv("ZWAI_UPGRADE_NEW_BINARY")
	if oldBinary == "" && newBinary == "" {
		t.Skip("set ZWAI_UPGRADE_OLD_BINARY and ZWAI_UPGRADE_NEW_BINARY for real cross-version acceptance")
	}
	oldHash := upgradeBinaryHash(t, oldBinary)
	newHash := upgradeBinaryHash(t, newBinary)
	if oldHash == newHash {
		t.Fatal("cross-version acceptance requires different executable bytes")
	}
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_NAME", "ZWAI_ENGINE_RESTORE_HELPER", "ZWAI_RESTORE_OLD", "ZWAI_RESTORE_DELAY"} {
		t.Setenv(name, "")
	}
	t.Logf("cross_version old_sha256=%s new_sha256=%s", oldHash, newHash)
	t.Run("active_worker_and_schema_migration", func(t *testing.T) {
		dir := t.TempDir()
		runRestartProcessAcceptance(t, dir, oldBinary, newBinary, true)
	})
	t.Run("original_unanswered_human_pause", func(t *testing.T) {
		dir := t.TempDir()
		runRestartAskProcessAcceptance(t, dir, oldBinary, newBinary, true)
	})
	t.Run("original_iteration_cap_pause", func(t *testing.T) {
		runUpgradeCapPause(t, oldBinary, newBinary)
	})
}

func handoffUpgradePrivateEngine(t *testing.T, dir, dataDir, newBinary string, old *restartProcess, oldRecord lease.Record) (*restartProcess, lease.Record) {
	t.Helper()
	began := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), handoffBudget)
	defer cancel()
	legacy, err := requestEngineHandoff(ctx, oldRecord.URL)
	if err != nil || legacy {
		t.Fatalf("historical engine did not acknowledge a durable handoff: legacy=%v error=%v", legacy, err)
	}
	// Both exact child PIDs remain Wait-owned even if the new listener never
	// publishes. The separate automatic-launcher gate covers shell detachment.
	if err := old.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
		if old.waitErr != nil {
			t.Fatalf("historical checkpointed child exited abnormally: %v", old.waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("historical checkpointed child did not exit")
	}
	replacement := startRestartProcess(t, dir, newBinary, "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	record := waitRestartRecord(t, dataDir, oldRecord.PID, restartBudget-time.Since(began), replacement)
	if record.PID != replacement.cmd.Process.Pid {
		t.Fatalf("private replacement lease belongs to another process: %+v", record)
	}
	t.Logf("cross_version durable_handoff old_pid=%d new_pid=%d listen_ms=%d", oldRecord.PID, record.PID, time.Since(began).Milliseconds())
	return replacement, record
}

func upgradeBinaryHash(t *testing.T, path string) string {
	t.Helper()
	if !filepath.IsAbs(path) {
		t.Fatal("both upgrade binary variables must name absolute executable paths")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("upgrade target is not a regular executable: %q (%v)", path, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func restartClassificationMatches(input restartModelInput, prompt, answer string) bool {
	if len(input.Messages) != 2 || input.Messages[0] == nil || input.Messages[1] == nil || input.Messages[0].Role != schema.System || input.Messages[0].Content != prompt || input.Messages[1].Role != schema.User || input.Stream || len(input.Tools) != 0 || input.ToolChoice != "none" {
		return false
	}
	var candidate struct {
		Messages []string `json:"messages"`
		Now      string   `json:"now"`
	}
	decoder := json.NewDecoder(strings.NewReader(input.Messages[1].Content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&candidate) != nil || decoder.Decode(new(any)) != io.EOF || len(candidate.Messages) != 1 || candidate.Messages[0] != answer {
		return false
	}
	_, err := time.Parse(time.RFC3339, candidate.Now)
	return err == nil
}

func assertUpgradePrivateTable(t *testing.T, dbPath string, want bool) {
	t.Helper()
	// Opening the newer Store here would migrate the old engine's live schema
	// and invalidate this test. This connection can only inspect private state.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pending_schedule_answers'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if (count == 1) != want {
		t.Fatalf("private candidate table exists=%v, want %v", count == 1, want)
	}
}

func assertUpgradeWorkerGeneration(t *testing.T, trace restartProcessTrace, workerID string) {
	t.Helper()
	generation, collectedGeneration := 0, 0
	restored := false
	var checkpointSeq int64
	waitCalls := map[string]bool{}
	for _, event := range trace.Events {
		if event.Kind == engine.KindRestartCheckpoint {
			checkpointSeq = max(checkpointSeq, event.Seq)
		}
		if event.AgentID == swarm.DefaultManagerID && event.Kind == "tool_call" && strings.HasPrefix(event.Text, "wait_agents(") {
			waitCalls[event.ToolCallID] = true
		}
	}
	for _, event := range trace.Events {
		if event.Kind == engine.KindRestartCheckpoint {
			var checkpoint struct {
				Version int                      `json:"version"`
				Workers []swarm.WorkerCheckpoint `json:"workers"`
			}
			if err := json.Unmarshal([]byte(event.Text), &checkpoint); err != nil || checkpoint.Version != 1 {
				t.Fatalf("old engine produced an unreadable checkpoint: %v", err)
			}
			for _, worker := range checkpoint.Workers {
				if worker.ID == workerID {
					if worker.Finished || worker.Generation <= 0 || generation != 0 && generation != worker.Generation {
						t.Fatalf("old active worker identity changed: %+v", worker)
					}
					generation = worker.Generation
				}
			}
		}
		if event.Kind == "engine_restore" {
			var receipt struct {
				Phase string `json:"phase"`
			}
			if json.Unmarshal([]byte(event.Text), &receipt) == nil {
				if receipt.Phase == "failed" || receipt.Phase == "cancelled" {
					t.Fatalf("upgrade failed restoration: %s", event.Text)
				}
				restored = restored || receipt.Phase == "restored"
			}
		}
		if event.AgentID == swarm.DefaultManagerID && event.Kind == "tool_result" && waitCalls[event.ToolCallID] && event.Seq > checkpointSeq && event.Err == "" {
			var result struct {
				Agents []struct {
					ID         string `json:"agent_id"`
					Status     string `json:"status"`
					Generation int    `json:"generation"`
				} `json:"agents"`
			}
			if json.Unmarshal([]byte(event.Text), &result) == nil {
				for _, agent := range result.Agents {
					if agent.ID == workerID && agent.Status == "done" {
						collectedGeneration = agent.Generation
					}
				}
			}
		}
	}
	if !restored || generation == 0 || collectedGeneration != generation {
		t.Fatalf("upgrade lost original worker generation or recovery: checkpoint=%d collected=%d restored=%v", generation, collectedGeneration, restored)
	}
	t.Logf("cross_version worker=%s original_generation=%d collected_generation=%d restored=%v", workerID, generation, collectedGeneration, restored)
}

// A restart must restore the existing permission wait, never manufacture a
// fresh manager allowance before the person presses Continue.
func runUpgradeCapPause(t *testing.T, oldBinary, newBinary string) {
	t.Helper()
	const finalText = "The confirmed continuation is retained."
	policyPrompt := scheduleAnswerProcessPrompt(t)
	var managers, allowed atomic.Int32
	reply := &restartProcessFixture{}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input restartModelInput
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" || json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Messages) == 0 {
			t.Error("iteration-cap fixture received an unexpected model request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if input.Messages[0].Content == policyPrompt {
			if !restartClassificationMatches(input, policyPrompt, finalText) {
				t.Error("iteration-cap fixture cannot approve an unknown candidate or protocol")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			reply.reply(w, false, `{"claims":[]}`, "", "", nil)
			return
		}
		if !input.Stream || len(input.Tools) == 0 {
			t.Error("iteration-cap fixture received a non-manager request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		step := managers.Add(1)
		if step == 1 {
			reply.reply(w, true, "", "original-cap-tool", "list_tasks", map[string]any{})
			return
		}
		if step != 2 || allowed.Load() != 1 {
			t.Error("restart bypassed the original iteration-cap permission")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply.reply(w, true, finalText, "", "", nil)
	}))
	t.Cleanup(fixture.Close)
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = []config.Provider{{ID: "upgrade-cap-fixture", BaseURL: fixture.URL, Model: "offline-upgrade-cap", FirstByteTimeoutSeconds: 40, TimeoutSeconds: 40, ContextWindow: 128000}}
	cfg.Models.Default = "upgrade-cap-fixture"
	cfg.Swarm.AutoTitle, cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false, false
	cfg.Swarm.ManagerMaxIterations = 1
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	old := startRestartProcess(t, dir, oldBinary, "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	oldRecord := waitRestartRecord(t, dataDir, 0, 15*time.Second, old)
	var created struct {
		Thread store.Thread `json:"thread"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads", map[string]string{"title": "Private iteration-cap upgrade"}, http.StatusCreated, &created)
	var started struct {
		Turn store.Turn `json:"turn"`
	}
	threadPath := "/api/threads/" + created.Thread.ID
	restartJSON(t, http.MethodPost, oldRecord.URL+threadPath+"/turns", map[string]string{"text": "Inspect the available tasks."}, http.StatusAccepted, &started)
	var live struct {
		Status struct {
			Running          bool   `json:"running"`
			AwaitingContinue bool   `json:"awaiting_continue"`
			TurnID           string `json:"turn_id"`
		} `json:"status"`
	}
	waitRestartCondition(t, 10*time.Second, "original iteration-cap permission wait", func() bool {
		restartJSON(t, http.MethodGet, oldRecord.URL+threadPath, nil, http.StatusOK, &live)
		return live.Status.Running && live.Status.AwaitingContinue && live.Status.TurnID == started.Turn.ID
	})
	_, replacement := handoffUpgradePrivateEngine(t, dir, dataDir, newBinary, old, oldRecord)
	var meta struct {
		Startup string `json:"startup"`
		Restore struct {
			Failed int `json:"failed"`
		} `json:"restore"`
	}
	waitRestartCondition(t, 10*time.Second, "restored iteration-cap permission wait", func() bool {
		restartJSON(t, http.MethodGet, replacement.URL+"/api/meta", nil, http.StatusOK, &meta)
		live.Status.Running, live.Status.AwaitingContinue, live.Status.TurnID = false, false, ""
		restartJSON(t, http.MethodGet, replacement.URL+threadPath, nil, http.StatusOK, &live)
		return meta.Startup == "ready" && live.Status.Running && live.Status.AwaitingContinue && live.Status.TurnID == started.Turn.ID
	})
	if meta.Restore.Failed != 0 || managers.Load() != 1 {
		t.Fatalf("upgrade failed or bypassed the original cap: failures=%d managers=%d", meta.Restore.Failed, managers.Load())
	}
	allowed.Store(1)
	restartJSON(t, http.MethodPost, replacement.URL+threadPath+"/continue", map[string]bool{"continue": true}, http.StatusAccepted, nil)
	var final restartProcessTrace
	waitRestartCondition(t, 10*time.Second, "human-authorized original turn completion", func() bool {
		final = restartTrace(t, replacement.URL, started.Turn.ID)
		return final.Turn.Status != store.TurnRunning
	})
	if final.Turn.ID != started.Turn.ID || final.Turn.Status != store.TurnDone || final.Turn.Final != finalText || managers.Load() != 2 {
		t.Fatalf("human continuation lost or replayed the original turn: turn=%+v managers=%d", final.Turn, managers.Load())
	}
	calls := 0
	for _, event := range final.Events {
		if event.Kind == "tool_call" && event.ToolCallID == "original-cap-tool" {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("upgrade repeated the already-confirmed cap tool: calls=%d", calls)
	}
	t.Logf("cross_version cap_turn=%s managers=%d original_tool_calls=%d restore_failed=%d", started.Turn.ID, managers.Load(), calls, meta.Restore.Failed)
}

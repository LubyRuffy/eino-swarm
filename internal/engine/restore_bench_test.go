package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const restoreBenchThread = "large"
const restoreBenchTurn = "original-turn"
const restoreBenchWorker = "original-worker"

// seedRestoreBenchmark retains the same 100,001 x 1,080 B tool history and
// two >2 MiB checkpoints for both snapshot and actual engine recovery.
func seedRestoreBenchmark(b *testing.B, st *store.Store) (int, int64) {
	b.Helper()
	events := make([]store.Event, 100001)
	body := strings.Repeat("result", 180)
	for i := range events {
		events[i] = store.Event{ThreadID: restoreBenchThread, TurnID: restoreBenchTurn, Seq: int64(i + 1), Kind: "tool_result", Text: body, CreatedAt: time.Now()}
	}
	if err := st.DB().CreateInBatches(events, 200).Error; err != nil {
		b.Fatal(err)
	}
	cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(strings.Repeat("context", 160000))}, Workers: []swarm.WorkerCheckpoint{{ID: restoreBenchWorker, Role: "inspector", Instruction: "continue existing task", History: []*schema.Message{schema.UserMessage(strings.Repeat("history", 160000))}}}}
	checkpoint, err := json.Marshal(cp)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ev := store.Event{ThreadID: restoreBenchThread, TurnID: restoreBenchTurn, Seq: int64(len(events) + i + 1), Kind: KindRestartCheckpoint, Text: string(checkpoint), CreatedAt: time.Now()}
		if err := st.DB().Create(&ev).Error; err != nil {
			b.Fatal(err)
		}
	}
	return len(checkpoint), int64(len(events) + 2)
}

// go test ./internal/engine -run '^$' -bench '^BenchmarkRestoreSnapshotLargeHistory$' -benchtime=3x -benchmem
func BenchmarkRestoreSnapshotLargeHistory(b *testing.B) {
	st, err := store.Open(filepath.Join(b.TempDir(), "history.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			b.Error(err)
		}
	}()
	checkpointBytes, _ := seedRestoreBenchmark(b, st)
	e := New(nil, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot, err := e.loadRestoreSnapshot(context.Background(), restoreBenchThread)
		if err != nil {
			b.Fatal(err)
		}
		if len(snapshot.events) != 1 || len(snapshot.running) != 1 || snapshot.running[0].ID != restoreBenchWorker {
			b.Fatalf("snapshot lost identity: %+v", snapshot)
		}
	}
	b.ReportMetric(float64(checkpointBytes), "checkpoint-bytes")
}

// go test ./internal/engine -run '^$' -bench '^BenchmarkRestoreToGenerationLargeHistory$' -benchtime=3x -benchmem
// Times actual Engine.Restore through its return and the offline HTTP adapter
// receiving the restored worker's unchanged MB-sized input. Fixture creation,
// worker cancellation/join, engine shutdown and server cleanup are excluded.
func BenchmarkRestoreToGenerationLargeHistory(b *testing.B) {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		b.Setenv(name, "")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		benchmarkRestoreToGeneration(b)
	}
	b.ReportMetric(1, "checkpoint-queries/op")
	b.ReportMetric(1, "recovery-scans/op")
}

func benchmarkRestoreToGeneration(b *testing.B) {
	b.Helper()
	cfg, err := config.Load(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	cfg.Swarm.AutoCompactTokens = 1 << 30
	var mu sync.Mutex
	observing, checkpoints, scans, fullScans := true, 0, 0, 0
	var generations []store.Event
	workerInput := make(chan struct{})
	invalidInput := make(chan error, 2)
	expectedHistory := strings.Repeat("history", 160000)
	var inputOnce sync.Once
	offline := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			select {
			case invalidInput <- err:
			default:
			}
			return
		}
		for _, message := range input.Messages {
			var content string
			if json.Unmarshal(message.Content, &content) == nil && content == expectedHistory {
				if input.Model != "offline-restore" {
					select {
					case invalidInput <- fmt.Errorf("unexpected model %q", input.Model):
					default:
					}
					return
				}
				inputOnce.Do(func() { mu.Lock(); observing = false; mu.Unlock(); close(workerInput) })
			}
		}
		// No model output is returned. Neither manager nor worker can execute a
		// tool while the benchmark joins the worker, then shuts down its manager.
		<-request.Context().Done()
	}))
	defer offline.Close()
	cfg.Models.Default = "offline-restore"
	cfg.Models.Providers = []config.Provider{{ID: "offline-restore", Model: "offline-restore", BaseURL: offline.URL + "/v1", ContextWindow: 1 << 30, TimeoutSeconds: 30, FirstByteTimeoutSeconds: 30}}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			b.Error(err)
		}
	}()
	if err := st.CreateThread(&store.Thread{ID: restoreBenchThread, ProviderID: cfg.Models.Default}); err != nil {
		b.Fatal(err)
	}
	if err := st.CreateTurn(&store.Turn{ID: restoreBenchTurn, ThreadID: restoreBenchThread, ProviderID: cfg.Models.Default, UserText: "continue the unfinished work"}); err != nil {
		b.Fatal(err)
	}
	checkpointBytes, tail := seedRestoreBenchmark(b, st)
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Shutdown()
	e.BeginRestore() // Deliberately never FinishRestore/StartScheduler.
	countRead := func(db *gorm.DB) {
		if db.Statement.Table != "events" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !observing {
			return
		}
		sql := db.Statement.SQL.String()
		if strings.Contains(sql, "kind =") && strings.Contains(sql, "ORDER BY seq desc") {
			checkpoints++
		}
		if strings.Contains(sql, "AND (kind IN") {
			scans++
		}
		if strings.Contains(sql, "seq >") && !strings.Contains(sql, "kind") {
			fullScans++
		}
	}
	if err := st.DB().Callback().Query().After("gorm:query").Register("restore-benchmark-reads", countRead); err != nil {
		b.Fatal(err)
	}
	if err := st.DB().Callback().Row().After("gorm:row").Register("restore-benchmark-rows", countRead); err != nil {
		b.Fatal(err)
	}
	if err := st.DB().Callback().Create().After("gorm:create").Register("restore-benchmark-generation", func(db *gorm.DB) {
		if event, ok := db.Statement.Dest.(*store.Event); ok && event.Kind == "generation_started" {
			mu.Lock()
			generations = append(generations, *event)
			mu.Unlock()
		}
	}); err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.StartTimer()
	if err := e.Restore(ctx, nil); err != nil {
		b.Fatal(err)
	}
	select {
	case <-workerInput:
	case err := <-invalidInput:
		b.Fatal("offline model input was invalid: ", err)
	case <-ctx.Done():
		b.Fatal("restoration never delivered the original worker input: ", ctx.Err())
	}
	b.StopTimer()
	rt := e.runtimeFor(restoreBenchThread)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	if reg == nil {
		b.Fatal("restored registry disappeared before cleanup")
	}
	reg.Cleanup() // Worker cancellation only: the manager input stays blocked.
	if err := reg.Wait(ctx, 5*time.Second); err != nil {
		b.Fatal("private worker did not exit: ", err)
	}
	e.Shutdown()
	mu.Lock()
	gotCheckpoints, gotScans, gotFull := checkpoints, scans, fullScans
	receipts := append([]store.Event(nil), generations...)
	mu.Unlock()
	if gotCheckpoints != 1 || gotScans != 1 || gotFull != 0 {
		b.Fatalf("checkpoint queries=%d recovery scans=%d full scans=%d", gotCheckpoints, gotScans, gotFull)
	}
	workerGeneration := false
	for _, event := range receipts {
		if event.ThreadID != restoreBenchThread || event.TurnID != restoreBenchTurn || (event.AgentID != restoreBenchWorker && event.AgentID != swarm.DefaultManagerID) {
			b.Fatalf("restored generation changed identity: %+v", event)
		}
		if event.AgentID == restoreBenchWorker {
			workerGeneration = true
		}
	}
	events, err := st.ListEvents(restoreBenchThread, tail, 0)
	if err != nil {
		b.Fatal(err)
	}
	restoredWorker := false
	for _, event := range events {
		if event.Kind == "tool_call" {
			b.Fatalf("benchmark replayed a tool beyond its model-input boundary: %+v", event)
		}
		if event.Kind == "spawned" {
			if event.AgentID != restoreBenchWorker || event.Role != "inspector" {
				b.Fatalf("restoration minted another worker: %+v", event)
			}
			restoredWorker = true
		}
	}
	turn, err := st.GetTurn(restoreBenchTurn)
	if err != nil || turn.Status != store.TurnRunning || !restoredWorker || !workerGeneration {
		b.Fatalf("restoration lost original identities: turn=%+v restored_worker=%v worker_generation=%v err=%v", turn, restoredWorker, workerGeneration, err)
	}
	if e.Status(restoreBenchThread).Running || e.Status(restoreBenchThread).Workers != 0 {
		b.Fatal("private runtime cleanup failed")
	}
	e.schedMu.Lock()
	schedulerStarted := e.schedStop != nil
	e.schedMu.Unlock()
	if schedulerStarted {
		b.Fatal("benchmark recovery started the scheduler")
	}
	b.ReportMetric(float64(checkpointBytes), "checkpoint-bytes")
}

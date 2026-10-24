package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// go test ./internal/engine -run '^$' -bench BenchmarkRestoreSnapshotLargeHistory -benchtime=3x -benchmem
// The history contains 100,001 1 KiB tool bodies and two checkpoints >2 MiB.
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
	events := make([]store.Event, 100001)
	body := strings.Repeat("result", 180)
	for i := range events {
		events[i] = store.Event{ThreadID: "large", TurnID: "original-turn", Seq: int64(i + 1), Kind: "tool_result", Text: body, CreatedAt: time.Now()}
	}
	if err := st.DB().CreateInBatches(events, 200).Error; err != nil {
		b.Fatal(err)
	}
	cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage(strings.Repeat("context", 160000))}, Workers: []swarm.WorkerCheckpoint{{ID: "original-worker", Role: "inspector", Instruction: "continue existing task", History: []*schema.Message{schema.UserMessage(strings.Repeat("history", 160000))}}}}
	checkpoint, _ := json.Marshal(cp)
	for i := 0; i < 2; i++ {
		ev := store.Event{ThreadID: "large", TurnID: "original-turn", Seq: int64(len(events) + i + 1), Kind: KindRestartCheckpoint, Text: string(checkpoint), CreatedAt: time.Now()}
		if err := st.DB().Create(&ev).Error; err != nil {
			b.Fatal(err)
		}
	}
	e := New(nil, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.ReportMetric(float64(len(checkpoint)), "checkpoint-bytes")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot, err := e.loadRestoreSnapshot(context.Background(), "large")
		if err != nil {
			b.Fatal(err)
		}
		if len(snapshot.events) != 1 || len(snapshot.running) != 1 || snapshot.running[0].ID != "original-worker" {
			b.Fatalf("snapshot lost identity: %+v", snapshot)
		}
	}
}

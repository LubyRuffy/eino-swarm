package engine

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Only tool body volume changes. Kept user/assistant text, event counts and
// per-turn identities are matched, so B/op exposes accidental payload reads.
func BenchmarkReplayHistoryToolBodies(b *testing.B) {
	for _, size := range []int{64, 32 << 10} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			cfg, err := config.Load(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			st, err := store.Open(filepath.Join(b.TempDir(), "history.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = st.Close() })
			e := New(cfg, st, provider.NewMock(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
			b.Cleanup(e.Shutdown)
			th := &store.Thread{ID: "benchmark-thread"}
			if err := st.CreateThread(th); err != nil {
				b.Fatal(err)
			}
			var messages []store.Message
			var events []store.Event
			body := strings.Repeat("T", size)
			for i := 0; i < 100; i++ {
				turn := fmt.Sprintf("turn-%d", i)
				answer := fmt.Sprintf("answer-%d 甲乙", i)
				for _, m := range []store.Message{{Role: "user", Content: fmt.Sprintf("input-%d 丙丁", i)}, {Role: "assistant", Content: answer}} {
					m.ThreadID = th.ID
					m.TurnID = turn
					m.Seq = int64(len(messages) + 1)
					messages = append(messages, m)
				}
				events = append(events, store.Event{ThreadID: th.ID, TurnID: turn, Seq: int64(len(events) + 1), Kind: "agent_message", AgentID: "manager", Text: answer})
				for j := 0; j < 16; j++ {
					messages = append(messages, store.Message{ThreadID: th.ID, TurnID: turn, Seq: int64(len(messages) + 1), Role: "tool", Content: body})
					events = append(events, store.Event{ThreadID: th.ID, TurnID: turn, Seq: int64(len(events) + 1), Kind: "tool_result", Text: body})
				}
			}
			if err := st.DB().CreateInBatches(messages, 100).Error; err != nil {
				b.Fatal(err)
			}
			if err := st.DB().CreateInBatches(events, 100).Error; err != nil {
				b.Fatal(err)
			}
			for _, legacy := range []bool{false, true} {
				b.Run(fmt.Sprintf("legacy=%v", legacy), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if legacy {
							if got := legacyReplayReference(b, e, th.ID, ""); len(got) != 200 {
								b.Fatal(len(got))
							}
						} else {
							got, err := e.replayHistory(th.ID)
							if err != nil || len(got) != 200 {
								b.Fatalf("history=%d err=%v", len(got), err)
							}
						}
					}
				})
			}
		})
	}
}

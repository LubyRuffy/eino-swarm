package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// The nil-default private overlay seam pauses the actual product AfterModel
// immediately before its SetHistory statement. It injects a possible scheduler
// pause; it neither substitutes Registry.exec nor invents a second history writer.
func TestRegistryRunWithJoinsRealAfterModelHistoryWriterOverHTTP(t *testing.T) {
	for _, capped := range []bool{false, true} {
		t.Run(fmt.Sprintf("length=%t", capped), func(t *testing.T) {
			const oldThought = "old-generation-private-thought"
			const oldFinal = "old-generation-complete"
			const nextHuman = "new-run-authoritative-input"
			const nextFinal = "new-run-verified-answer"
			var requestCount atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []*schema.Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				step := requestCount.Add(1)
				delta, finish := map[string]any{"content": nextFinal}, "stop"
				if step == 1 {
					if capped {
						delta, finish = map[string]any{"reasoning_content": oldThought}, "length"
					} else {
						delta = map[string]any{"content": oldFinal}
					}
				} else {
					seen := false
					for _, m := range input.Messages {
						if m != nil && m.Role == schema.User && m.Content == nextHuman {
							seen = true
						}
					}
					if !seen {
						t.Error("second real HTTP model input did not carry the new run's authoritative message")
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
					b, _ := json.Marshal(map[string]any{"id": "offline-history", "object": "chat.completion.chunk", "choices": []any{choice}})
					_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
				}
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer endpoint.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			m, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{BaseURL: endpoint.URL, APIKey: "offline-fixture", Model: "offline-history-boundary"})
			if err != nil {
				t.Fatal(err)
			}
			reg := NewRegistry()
			defer reg.Close()
			reg.ModelBuilder = func(string, string) model.BaseChatModel { return m }
			entered, release, writerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce, enterOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			reg.testBeforeAfterModelHistory = func(messages []*schema.Message) func() {
				if len(messages) == 0 {
					return nil
				}
				last := messages[len(messages)-1]
				if last == nil || last.Content != oldFinal && !strings.Contains(last.ReasoningContent, oldThought) {
					return nil
				}
				enterOnce.Do(func() { close(entered) })
				<-release
				return func() { close(writerDone) }
			}
			type outcome struct {
				result RunResult
				err    error
			}
			first := make(chan outcome, 1)
			go func() {
				result, err := reg.RunWith(ctx, outputRecoveryJoinFixtureConfig("old-run-input"), nil)
				first <- outcome{result, err}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("real product AfterModel did not reach the injected scheduler pause")
			}
			var firstResult outcome
			early := false
			select {
			case firstResult = <-first:
				early = true
			case <-time.After(150 * time.Millisecond):
			}
			if !capped && early {
				unblock()
				<-writerDone
				t.Fatalf("normal completion returned before the real AfterModel history writer: %v", firstResult.err)
			}
			var secondResult RunResult
			var secondErr error
			if early {
				secondResult, secondErr = reg.RunWith(ctx, outputRecoveryJoinFixtureConfig(nextHuman), nil)
				if secondErr != nil || secondResult.Final != nextFinal {
					unblock()
					<-writerDone
					t.Fatalf("the next actual Registry/HTTP run failed: final=%q err=%v", secondResult.Final, secondErr)
				}
				if !historyHasFixtureMessage(reg.historySnapshot(), nextHuman) {
					unblock()
					<-writerDone
					t.Fatal("the next actual run never installed its history before the old writer was released")
				}
			}
			unblock()
			select {
			case <-writerDone:
			case <-ctx.Done():
				t.Fatal("the real old AfterModel writer did not finish after release")
			}
			if !early {
				select {
				case firstResult = <-first:
				case <-ctx.Done():
					t.Fatal("first Registry run did not join its old writer")
				}
			}
			if capped {
				var limit *ReasoningOutputLimit
				if !errors.As(firstResult.err, &limit) {
					t.Fatalf("HTTP length stop lost the actual typed cap: %v", firstResult.err)
				}
			} else if firstResult.err != nil || firstResult.result.Final != oldFinal {
				t.Fatalf("normal control lost its final reply: %+v", firstResult)
			}
			if !early {
				secondResult, secondErr = reg.RunWith(ctx, outputRecoveryJoinFixtureConfig(nextHuman), nil)
			}
			history := reg.historySnapshot()
			clobbered := !historyHasFixtureMessage(history, nextHuman) && historyHasFixtureMessage(history, "old-run-input")
			t.Logf("injected_scheduler_pause=true capped=%t first_returned_while_writer_blocked=%t next_actual_HTTP_final=%q old_SetHistory_overwrote_next_history=%t writer_joined=true", capped, early, secondResult.Final, clobbered)
			if capped && early && clobbered {
				t.Fatal("typed cap returned before the actual old AfterModel writer; releasing that writer overwrote a completed next Registry/HTTP run's history")
			}
			if early {
				t.Fatal("RunWith returned while its actual AfterModel writer was still paused")
			}
			if secondErr != nil || secondResult.Final != nextFinal || !historyHasFixtureMessage(history, nextHuman) {
				t.Fatalf("joined next run did not retain authoritative history: final=%q err=%v", secondResult.Final, secondErr)
			}
			// Both real RunWith calls and the old writer have joined before observer cleanup.
			reg.testBeforeAfterModelHistory = nil
		})
	}
}

func historyHasFixtureMessage(messages []*schema.Message, content string) bool {
	for _, message := range messages {
		if message != nil && message.Content == content {
			return true
		}
	}
	return false
}

// Enable the new opt-in when present; the unchanged old baseline still runs
// the behavioral RED instead of failing to compile for a missing field.
func outputRecoveryJoinFixtureConfig(task string) RunConfig {
	cfg := RunConfig{Instruction: "Complete the current request.", Task: task, MaxIterations: 100}
	field := reflect.ValueOf(&cfg).Elem().FieldByName("CaptureReasoningRecoveryInput")
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.Bool {
		field.SetBool(true)
	}
	return cfg
}

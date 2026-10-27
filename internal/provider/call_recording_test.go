package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// A queued request may be admitted by a later manager turn. Its earlier wait
// and its actual HTTP call must not be conflated into one ownership instant.
func TestCallRecorderIsSelectedAfterBackoffAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(time.Second, 5*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		owner, billed, waited := "first", "", ""
		ctx := WithCallRecorders(context.Background(), func() Recorder {
			selected := owner
			return func(CallRecord) { billed = selected }
		}, func(record CallRecord) {
			if record.Backoff == nil {
				t.Fatal("actual request was sent to the wait observer")
			}
			waited, owner = owner, "second"
		})
		cm := &recordingModel{gate: gate, inner: &fakeModel{out: schema.AssistantMessage("verified", nil)}, rec: func(CallRecord) { t.Fatal("stale recorder used") }}
		if _, err := cm.Generate(ctx, []*schema.Message{schema.UserMessage("Inspect the evidence.")}); err != nil {
			t.Fatal(err)
		}
		if waited != "first" || billed != "second" {
			t.Fatalf("wait=%q request=%q", waited, billed)
		}
	})
}

func TestStreamRecorderKeepsItsRequestOwnerUntilTerminalPersistence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		owner, billed := "first", ""
		ctx = WithCallRecorders(ctx, func() Recorder {
			selected := owner
			return func(CallRecord) { billed = selected }
		}, nil)
		cm := &recordingModel{inner: &terminalStreamModel{cause: context.Canceled, waitCancel: true}, rec: func(CallRecord) { t.Fatal("stale recorder used") }}
		stream, err := cm.Stream(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		owner = "second"
		if _, err := stream.Recv(); err != nil {
			t.Fatal(err)
		}
		cancel()
		_, _ = stream.Recv()
		if billed != "first" {
			t.Fatalf("terminal callback moved the original stream to %q", billed)
		}
	})
}

func TestCallRecordingDefaultsRemainUsableWithoutAnOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "nil factory"}[override], func(t *testing.T) {
			calls := 0
			recorder := func(CallRecord) { calls++ }
			cm := &recordingModel{rec: recorder}
			ctx := context.Background()
			if override {
				ctx = WithCallRecorders(ctx, nil, recorder)
			}
			cm.requestRecorder(ctx)(CallRecord{})
			cm.backoffRecorder(ctx)(CallRecord{Err: io.EOF})
			if calls != 2 {
				t.Fatalf("static recorder changed: %d", calls)
			}
		})
	}
}

// An optional-effort rejection and its fallback are distinct HTTP attempts.
// Each one freezes its own recorder after admission, rather than sharing one
// snapshot from the outer Generate/Stream call or choosing at completion.
func TestOptionalEffortFallbackSelectsAndFreezesEachHTTPRequestRecorder(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		name := "generate"
		if streamed {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var owner, requests, staticRecords atomic.Int32
			owner.Store(1)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Effort string `json:"reasoning_effort"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if requests.Add(1) == 1 {
					if body.Effort != config.ReasoningLow {
						t.Errorf("first request omitted its optional effort: %q", body.Effort)
					}
					owner.Store(2)
					http.Error(w, `{"error":{"message":"unknown parameter: reasoning_effort","type":"invalid_request_error"}}`, http.StatusBadRequest)
					return
				}
				if body.Effort != "" {
					t.Errorf("fallback retained rejected effort: %q", body.Effort)
				}
				owner.Store(3)
				if streamed {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, backoffChunk+"data: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, backoffCompletion)
				}
			}))
			t.Cleanup(endpoint.Close)
			build, err := backoffPool(endpoint.URL).AuxiliaryModelBuilder(t.Context(), "fixture", "", config.ReasoningLow, func(CallRecord) { staticRecords.Add(1) })
			if err != nil {
				t.Fatal(err)
			}
			type attributedCall struct {
				owner  int32
				record CallRecord
			}
			recorded := make(chan attributedCall, 2)
			ctx := WithCallRecorders(t.Context(), func() Recorder {
				selected := owner.Load()
				return func(record CallRecord) { recorded <- attributedCall{selected, record} }
			}, nil)
			cm := build("policy", "policy")
			if streamed {
				sr, err := cm.Stream(ctx, backoffMessages())
				if err != nil {
					t.Fatal(err)
				}
				defer sr.Close()
				if err := finishBackoffStream(sr); err != nil {
					t.Fatal(err)
				}
			} else if _, err := cm.Generate(ctx, backoffMessages()); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 2 || len(recorded) != 2 || staticRecords.Load() != 0 {
				t.Fatalf("fallback recording: requests=%d calls=%d stale=%d", requests.Load(), len(recorded), staticRecords.Load())
			}
			first, second := <-recorded, <-recorded
			if first.owner != 1 || HTTPStatus(first.record.Err) != http.StatusBadRequest || second.owner != 2 || second.record.Err != nil {
				t.Fatalf("HTTP attempts lost their starting owners: first=%+v second=%+v", first, second)
			}
		})
	}
}

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// effortRejectingServer answers like an endpoint without reasoning support:
// any request carrying reasoning_effort is a 400. It records each request's
// effort ("" when absent) in arrival order.
func effortRejectingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Effort string `json:"reasoning_effort"`
			Stream bool   `json:"stream"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		seen = append(seen, body.Effort)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if body.Effort != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"unknown parameter: reasoning_effort","type":"invalid_request_error"}}`)
			return
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, backoffChunk+"data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, backoffCompletion)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// Background classifiers and memory review default to a low effort to stay
// off the answer's critical path. An endpoint that has no reasoning control
// rejects the field outright; that must not turn every classification into a
// failure, and the refused attempt must stay visible in Trace.
func TestAuxiliaryCallRetriesWithoutAnEffortTheEndpointRejects(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		srv, seen := effortRejectingServer(t)
		pool := backoffPool(srv.URL)
		s := &sink{}
		build, err := pool.AuxiliaryModelBuilder(context.Background(), "fixture", "", config.ReasoningLow, s.rec())
		if err != nil {
			t.Fatal(err)
		}
		m := build("policy", "policy")
		var text string
		if streamed {
			stream, err := m.Stream(context.Background(), backoffMessages())
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			msg, err := stream.Recv()
			if err != nil {
				t.Fatalf("stream recv: %v", err)
			}
			text = msg.Content
			if err := finishBackoffStream(stream); err != nil {
				t.Fatal(err)
			}
			stream.Close()
		} else {
			out, err := m.Generate(context.Background(), backoffMessages())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			text = out.Content
		}
		if text != "ready" {
			t.Fatalf("streamed=%v: answer %q", streamed, text)
		}
		if got := seen(); len(got) != 2 || got[0] != config.ReasoningLow || got[1] != "" {
			t.Fatalf("streamed=%v: requests carried efforts %q, want [low, none]", streamed, got)
		}
		calls := s.all()
		if len(calls) != 2 || calls[0].Err == nil || HTTPStatus(calls[0].Err) != http.StatusBadRequest || calls[1].Err != nil {
			t.Fatalf("streamed=%v: the refused attempt must be recorded before the retry: %+v", streamed, calls)
		}
	}
}

// The human's chosen thinking level is not optional: a conversation model
// that rejects it reports the error rather than silently thinking less.
func TestConversationEffortIsNotDroppedOnRejection(t *testing.T) {
	srv, seen := effortRejectingServer(t)
	build, err := backoffPool(srv.URL).ModelBuilder(context.Background(), "fixture", "", config.ReasoningHigh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build("manager", "manager").Generate(context.Background(), []*schema.Message{schema.UserMessage("go")}); HTTPStatus(err) != http.StatusBadRequest {
		t.Fatalf("err=%v, want the endpoint's 400", err)
	}
	if got := seen(); len(got) != 1 {
		t.Fatalf("requests=%q, a chosen level must not be retried without it", got)
	}
}

// Other rejections are not about the effort; asking again would only double
// the failure.
func TestAuxiliaryCallDoesNotRetryOtherFailures(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key","type":"auth"}}`)
	}))
	t.Cleanup(srv.Close)
	build, err := backoffPool(srv.URL).AuxiliaryModelBuilder(context.Background(), "fixture", "", config.ReasoningLow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build("policy", "policy").Generate(context.Background(), backoffMessages()); HTTPStatus(err) != http.StatusUnauthorized {
		t.Fatalf("err=%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
}

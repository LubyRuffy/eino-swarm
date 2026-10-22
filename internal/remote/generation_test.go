package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGenerationProjectionPreservesStructuredCauseAndOutputIdentityAtSmallTextLimit(t *testing.T) {
	for _, kind := range []string{"generation_interrupted", "generation_recovered"} {
		body := `{"reason":"model_error","detail":"` + strings.Repeat("safe detail ", 80) + `","recovery":"pending"}`
		view := eventView(store.Event{ThreadID: "thread", TurnID: "turn", Seq: 7, Kind: kind, AgentID: "worker", MessageID: "output", Text: body}, config.RemoteConfig{EventChars: 8})
		var parsed struct{ Reason, Detail, Recovery string }
		if err := json.Unmarshal([]byte(view.Text), &parsed); err != nil {
			t.Fatalf("phone received clipped broken JSON: %q %v", view.Text, err)
		}
		if parsed.Reason != "model_error" || parsed.Recovery != "pending" || view.MessageID != "output" || len([]rune(parsed.Detail)) > 8 {
			t.Fatalf("projection lost lifecycle identity: %+v %+v", view, parsed)
		}
	}
}

// A real encrypted Pairlink watch must receive engine lifecycle events. A
// mobile mock or direct projection test cannot prove the subscription list.
func TestGenerationInterruptedReasonIsPushedOverActualPairlinkWatch(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Checking the current evidence.\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "generation-fixture"
	cfg.Models.Providers = []config.Provider{{ID: "generation-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.AutoReview = false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := engine.New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	link := newFollowupRelay(t, e)
	if got := link.rpc(t, Request{ID: "watch-generation", Op: OpWatch, ThreadID: th.ID}); !got.OK {
		t.Fatalf("watch=%+v", got)
	}
	if got := link.rpc(t, Request{ID: "send-generation", Op: OpSend, ThreadID: th.ID, Text: "Inspect the current evidence."}); !got.OK {
		t.Fatalf("send=%+v", got)
	}
	deadline := time.After(5 * time.Second)
	var started, interrupted *EventView
	for interrupted == nil {
		for _, resp := range link.seen {
			if resp.Op == OpEvent && resp.Event != nil {
				if resp.Event.Kind == "generation_started" {
					started = resp.Event
				}
				if resp.Event.Kind == "generation_interrupted" {
					interrupted = resp.Event
				}
			}
		}
		if interrupted == nil {
			link.receive(t, deadline)
		}
	}
	if started == nil || started.MessageID == "" || started.MessageID != interrupted.MessageID || started.TurnID != interrupted.TurnID {
		t.Fatalf("Pairlink lost request identity: started=%+v interrupted=%+v", started, interrupted)
	}
	var body struct{ Reason, Recovery string }
	if err := json.Unmarshal([]byte(interrupted.Text), &body); err != nil {
		t.Fatal(err)
	}
	if body.Reason != "output_limit" || body.Recovery != "none" {
		t.Fatalf("Pairlink cause=%+v", body)
	}
}

func TestGenerationCauseClipsOnlyBoundedDetailAndKeepsLegacyPayload(t *testing.T) {
	text := `{"reason":"model_error","detail":"` + strings.Repeat("safe", 200) + `","recovery":"none"}`
	var body struct{ Reason, Detail, Recovery string }
	if err := json.Unmarshal([]byte(clipGenerationCause(text, 4000)), &body); err != nil {
		t.Fatal(err)
	}
	if len([]rune(body.Detail)) != 512 || body.Reason != "model_error" || body.Recovery != "none" {
		t.Fatalf("invalid bounded cause: %+v", body)
	}
	if got := clipGenerationCause("legacy unknown cause", 8); got != "legacy unknown cause" {
		t.Fatalf("legacy cause rewritten: %q", got)
	}
}

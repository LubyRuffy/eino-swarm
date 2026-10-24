package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Queue through encrypted phone frames while the actual SDK request is held.
// A direct Handle call or a mock model cannot prove this handoff reaches watch.
func TestFollowupAfterErrorOverPairlinkRunsHumanFIFOAndRetainsFailure(t *testing.T) {
	const completed = "The inspection completed."
	const failure = "fixture request rejected"
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var mu sync.Mutex
	var inputs []string
	policies := 0
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected SDK request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "invalid offline request", http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage `json:"tools"`
			Stream   bool              `json:"stream"`
			Choice   string            `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request JSON", http.StatusBadRequest)
			return
		}
		if len(request.Messages) == 2 && request.Messages[0].Role == "system" && strings.HasPrefix(request.Messages[0].Content, "## Schedule answer claims\n") {
			var candidate struct {
				Messages []string `json:"messages"`
				Now      string   `json:"now"`
			}
			err := json.Unmarshal([]byte(request.Messages[1].Content), &candidate)
			_, clockErr := time.Parse(time.RFC3339, candidate.Now)
			if err != nil || clockErr != nil || request.Messages[1].Role != "user" || request.Stream || len(request.Tools) != 0 || request.Choice != "none" || !reflect.DeepEqual(candidate.Messages, []string{completed}) {
				t.Error("schedule classification changed protocol or its fixed candidate")
				http.Error(w, "unexpected classification", http.StatusBadRequest)
				return
			}
			mu.Lock()
			policies++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"{\"claims\":[]}"},"finish_reason":"stop"}]}`)
			return
		}
		if len(request.Tools) == 0 || !request.Stream {
			t.Error("expected an actual streaming manager request with tools")
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		last := ""
		for _, message := range request.Messages {
			if message.Role == "user" {
				last = message.Content
			}
		}
		mu.Lock()
		inputs = append(inputs, last)
		n := len(inputs)
		mu.Unlock()
		if n == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":{"message":%q,"type":"invalid_request_error","code":"invalid_request"}}`, failure)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", completed)
	}))
	t.Cleanup(endpoint.Close)
	t.Cleanup(unblock)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.Models.Default = "relay-fixture"
		c.Models.Providers = []config.Provider{{ID: "relay-fixture", BaseURL: endpoint.URL, Model: "offline", TimeoutSeconds: 4, FirstByteTimeoutSeconds: 2}}
		c.Swarm.AutoTitle = false
		c.Memory.Enabled, c.Memory.AutoReview = false, false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := engine.New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	th, err := e.CreateThread("paired queue recovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	if got := r.rpc(t, Request{ID: "watch", Op: OpWatch, ThreadID: th.ID}); !got.OK || got.Op != OpReady {
		t.Fatalf("encrypted watch: %+v", got)
	}
	const original = "Inspect the initial state."
	if got := r.rpc(t, Request{ID: "initial", Op: OpSend, ThreadID: th.ID, Text: original}); !got.OK {
		t.Fatalf("encrypted initial send: %+v", got)
	}
	first := r.userEvent(t, original)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the actual Engine did not reach the HTTP model")
	}
	queued := []string{"Inspect the next state.", "Inspect the remaining state."}
	var queueIDs []string
	for i, text := range queued {
		got := r.rpc(t, Request{ID: fmt.Sprintf("queue-%d", i), Op: OpSend, ThreadID: th.ID, Text: text})
		if !got.OK || got.Followups == nil || len(*got.Followups) != i+1 {
			t.Fatalf("encrypted human request was not queued: %+v", got)
		}
		row := (*got.Followups)[i]
		if row.Text != text || row.ID == "" {
			t.Fatalf("queued request lost its identity: %+v", row)
		}
		queueIDs = append(queueIDs, row.ID)
	}
	unblock()
	deadline := time.After(5 * time.Second)
	for {
		got := r.receive(t, deadline)
		if got.Event != nil && got.Event.TurnID == first.TurnID && got.Event.Kind == swarm.NotifyError.String() {
			if !strings.Contains(got.Event.Err, failure) || got.Status == nil {
				t.Fatalf("original failure did not reach the phone watch: %+v", got)
			}
			break
		}
	}
	failed, err := st.GetTurn(first.TurnID)
	// Error is published before FinishTurn commits; a phone event is not
	// the database's terminal acknowledgement.
	for until := time.Now().Add(4 * time.Second); err == nil && failed.Status != store.TurnError && time.Now().Before(until); {
		time.Sleep(5 * time.Millisecond)
		failed, err = st.GetTurn(first.TurnID)
	}
	if err != nil || failed.Status != store.TurnError || !strings.Contains(failed.Error, failure) {
		t.Fatalf("original failed turn: %+v err=%v", failed, err)
	}
	trace, err := st.ListTurnEvents(first.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(4 * time.Second)
	var turns []store.Turn
	for time.Now().Before(until) {
		turns, err = st.ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) == 3 && turns[2].Status == store.TurnDone && !e.Status(th.ID).Running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(turns) != 3 || turns[1].UserText != queued[0] || turns[2].UserText != queued[1] || turns[1].Status != store.TurnDone || turns[2].Status != store.TurnDone {
		rows, _ := e.ListFollowups(th.ID)
		t.Fatalf("paired human FIFO stalled after real model failure: turns=%+v queue=%+v", turns, rows)
	}
	for _, turn := range turns[1:] {
		if turn.ScheduleContinue || turn.GoalContinue || turn.ScheduleRunID != "" {
			t.Fatalf("a queued human request became an automatic retry: %+v", turn)
		}
		if event := r.userEvent(t, turn.UserText); event.TurnID != turn.ID {
			t.Fatalf("phone user event did not identify its independent turn: %+v", event)
		}
	}
	deadline = time.After(5 * time.Second)
	for {
		complete := false
		for _, got := range r.seen {
			if got.Event != nil && got.Event.Kind == swarm.NotifyDone.String() && got.Event.TurnID == turns[2].ID && got.Status != nil && got.Status.Followups != nil && len(*got.Status.Followups) == 0 {
				complete = true
			}
		}
		if complete {
			break
		}
		r.receive(t, deadline)
	}
	rows, err := e.ListFollowups(th.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("phone queue was not consumed: %+v err=%v", rows, err)
	}
	after, err := st.ListTurnEvents(first.TurnID)
	if err != nil || !reflect.DeepEqual(after, trace) || !reflect.DeepEqual(turns[0], *failed) {
		t.Fatalf("human handoff rewrote the original failure or Trace: err=%v", err)
	}
	terminal := 0
	for _, event := range after {
		if event.Kind == swarm.NotifyDone.String() || event.Kind == engine.KindModelRetry {
			t.Fatalf("original model failure became a success or retry: %+v", event)
		}
		if event.Kind == swarm.NotifyError.String() {
			terminal++
			if event.Err != failed.Error {
				t.Fatalf("original Trace lost its exact error: %+v", event)
			}
		}
	}
	mu.Lock()
	gotInputs, gotPolicies := append([]string(nil), inputs...), policies
	mu.Unlock()
	if terminal != 1 || !reflect.DeepEqual(gotInputs, append([]string{original}, queued...)) || gotPolicies != 2 || queueIDs[0] == queueIDs[1] {
		t.Fatalf("duplicate or missing real request: errors=%d inputs=%q policies=%d queueIDs=%q", terminal, gotInputs, gotPolicies, queueIDs)
	}
}

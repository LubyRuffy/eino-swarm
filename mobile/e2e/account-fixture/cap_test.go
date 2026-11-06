package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"gorm.io/gorm"
)

// This crosses the actual relay, Host, RPC and Eino manager boundary. Native
// harnesses get only the same input/observation controls used here; the caller
// must supply the encrypted Continue/Stop decisions.
func TestNativeCapFixtureWaitsForRealEncryptedPhoneDecisions(t *testing.T) {
	phone, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	f := realUpgradeFixture(t, phone.Public())
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	offered, err := f.host.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := protocol.Parse(offered.URI)
	if err != nil {
		t.Fatal(err)
	}
	ticket, hostPub, sid, err := client.RedeemOffer(ctx, offer, phone)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := client.Dial(ctx, client.Config{HubURL: f.origin, Identity: phone, Token: ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	link, err := peer.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		t.Fatal(err)
	}
	first := readCapHTTP(t, f, http.MethodPost, "/api/cap/start")
	paused := waitCapHTTP(t, f, func(s capObservation) bool { return s.AwaitingContinue && s.MaxIterationsSeq > 0 })
	if first.ThreadID == "" || first.TurnID == "" || paused.ThreadID != first.ThreadID || paused.TurnID != first.TurnID || paused.TurnCount != 1 || paused.Title != "Native cap 1" {
		t.Fatalf("cap input lost its original identity: %+v", paused)
	}
	if paused.ContinuedCount != 0 || paused.ManagerGenerationAfterContinue || f.engine.Config().SwarmSnapshot().ManagerMaxIterations != 1 {
		t.Fatalf("fixture fabricated a continuation: %+v", paused)
	}
	opened := capPhoneRPC(t, ctx, link, remote.Request{ID: "open-cap", Op: remote.OpOpen, ThreadID: paused.ThreadID})
	if !opened.OK || opened.Detail == nil || opened.Detail.Running == nil || !opened.Detail.Running.AwaitingContinue || opened.Detail.Running.TurnID != paused.TurnID {
		t.Fatalf("real Host did not expose the native cap: %+v", opened)
	}
	proceed := true
	if response := capPhoneRPC(t, ctx, link, remote.Request{ID: "continue-cap", Op: remote.OpContinue, ThreadID: paused.ThreadID, Continue: &proceed}); !response.OK {
		t.Fatalf("phone Continue failed: %+v", response)
	}
	resumed := waitCapHTTP(t, f, func(s capObservation) bool {
		return s.ManagerGenerationAfterContinue && (s.AwaitingContinue || !s.Running)
	})
	if resumed.TurnID != paused.TurnID || resumed.TurnCount != 1 || resumed.ContinuedCount != 1 || resumed.MaxIterationsContinuedSeq <= paused.MaxIterationsSeq || resumed.GenerationCount < 2 {
		t.Fatalf("Continue did not run another generation on the original turn: %+v", resumed)
	}
	if resumed.Running {
		proceed = false
		if response := capPhoneRPC(t, ctx, link, remote.Request{ID: "stop-resumed-cap", Op: remote.OpContinue, ThreadID: paused.ThreadID, Continue: &proceed}); !response.OK {
			t.Fatalf("phone Stop failed: %+v", response)
		}
		waitCapHTTP(t, f, func(s capObservation) bool {
			return !s.Running && s.TurnStatus == store.TurnCancelled && s.TerminalSeq > 0
		})
	}
	second := readCapHTTP(t, f, http.MethodPost, "/api/cap/start")
	paused = waitCapHTTP(t, f, func(s capObservation) bool { return s.AwaitingContinue && s.MaxIterationsSeq > 0 })
	if second.ThreadID == first.ThreadID || second.TurnID == first.TurnID || paused.Title != "Native cap 2" || paused.TurnCount != 1 {
		t.Fatalf("new native case reused the previous turn: %+v", paused)
	}
	proceed = false
	if response := capPhoneRPC(t, ctx, link, remote.Request{ID: "decline-cap", Op: remote.OpContinue, ThreadID: paused.ThreadID, Continue: &proceed}); !response.OK {
		t.Fatalf("phone decline failed: %+v", response)
	}
	stopped := waitCapHTTP(t, f, func(s capObservation) bool {
		return !s.Running && s.TurnStatus == store.TurnCancelled && s.TerminalSeq > 0
	})
	if stopped.TurnID != paused.TurnID || stopped.TurnCount != 1 || stopped.ContinuedCount != 0 || stopped.AwaitingContinue {
		t.Fatalf("Stop fabricated a new turn or continuation: %+v", stopped)
	}
}

func capPhoneRPC(t *testing.T, ctx context.Context, link *client.Link, request remote.Request) remote.Response {
	t.Helper()
	request.V = remote.ProtocolV
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := link.Send(raw); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-link.Recv():
		var response remote.Response
		if err := json.Unmarshal(raw, &response); err != nil || response.ID != request.ID {
			t.Fatalf("invalid cap RPC response: %v", err)
		}
		return response
	case <-ctx.Done():
		t.Fatal("cap RPC did not return")
		return remote.Response{}
	}
}

func readCapHTTP(t *testing.T, f *fixture, method, path string) capObservation {
	t.Helper()
	response := httptest.NewRecorder()
	f.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("cap route returned %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Data capObservation `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

func waitCapHTTP(t *testing.T, f *fixture, ready func(capObservation) bool) capObservation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var state capObservation
	for time.Now().Before(deadline) {
		state = readCapHTTP(t, f, http.MethodGet, "/api/cap/state")
		if ready(state) {
			return state
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cap fixture did not reach expected state: %+v", state)
	return state
}

func localCapFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := engine.New(cfg, db, provider.NewMock(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(eng.Shutdown)
	return &fixture{engine: eng, counts: map[string]int{}}
}

func TestNativeCapFixtureRefusesInvalidControlsAndUnavailableObservations(t *testing.T) {
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/cap/start", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/cap/state", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/cap/start", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/cap/state", http.StatusServiceUnavailable},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			f := &fixture{counts: map[string]int{}}
			response := httptest.NewRecorder()
			f.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d", response.Code, test.status)
			}
		})
	}
	f := localCapFixture(t)
	if state := readCapHTTP(t, f, http.MethodGet, "/api/cap/state"); state.ThreadID != "" || state.Running || state.AwaitingContinue || state.TurnCount != 0 {
		t.Fatalf("unused fixture invented a cap: %+v", state)
	}
	thread, err := f.engine.CreateThread("missing original cap turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f.capThreadID, f.capTurnID = thread.ID, "missing"
	response := httptest.NewRecorder()
	f.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/cap/state", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("missing original turn was accepted")
	}
	if err := f.engine.Store().Close(); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	f.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/cap/state", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("closed storage fabricated a successful observation")
	}
}

func TestNativeCapFixtureReportsInputFailuresWithoutMakingAPause(t *testing.T) {
	for _, stage := range []string{"configuration", "thread", "turn"} {
		t.Run(stage, func(t *testing.T) {
			f := localCapFixture(t)
			if stage == "configuration" {
				path := f.engine.Config().Path()
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				table := stage + "s"
				if err := f.engine.Store().DB().Callback().Create().Before("gorm:create").Register("cap_input_failure", func(db *gorm.DB) {
					if db.Statement.Table == table {
						_ = db.AddError(errors.New("private cap input failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			response := httptest.NewRecorder()
			f.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/cap/start", nil))
			if response.Code != http.StatusServiceUnavailable || f.capStarts != 0 || f.capTurnID != "" {
				t.Fatalf("failed input fabricated a cap: %d / %d", response.Code, f.capStarts)
			}
		})
	}
}

func TestNativeCapObservationUsesOnlyOriginalTurnManagerReceipts(t *testing.T) {
	f := localCapFixture(t)
	thread, err := f.engine.CreateThread("durable cap observation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	original := &store.Turn{ThreadID: thread.ID, Status: store.TurnDone}
	other := &store.Turn{ThreadID: thread.ID, Status: store.TurnDone}
	for _, turn := range []*store.Turn{original, other} {
		if err := f.engine.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
	}
	f.capThreadID, f.capTurnID, f.capTitle = thread.ID, original.ID, thread.Title
	for _, event := range []store.Event{
		{TurnID: original.ID, AgentID: "manager", Kind: "generation_started"},
		{TurnID: original.ID, AgentID: "manager", Kind: "max_iterations"},
		{TurnID: other.ID, AgentID: "manager", Kind: "max_iterations_continued"},
		{TurnID: original.ID, AgentID: "worker", Kind: "max_iterations_continued"},
		{TurnID: original.ID, AgentID: "manager", Kind: "max_iterations_continued"},
		{TurnID: original.ID, AgentID: "manager", Kind: "generation_started"},
		{TurnID: original.ID, AgentID: "manager", Kind: "done"},
	} {
		event.ThreadID = thread.ID
		if err := f.engine.Store().AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	state := readCapHTTP(t, f, http.MethodGet, "/api/cap/state")
	if state.TurnCount != 2 || state.TurnStatus != store.TurnDone || state.MaxIterationsSeq != 2 || state.MaxIterationsContinuedSeq != 5 || state.ContinuedCount != 1 || state.GenerationCount != 2 || !state.ManagerGenerationAfterContinue || state.TerminalSeq != 7 {
		t.Fatalf("observation mixed another turn or worker into manager receipts: %+v", state)
	}
	// No runtime actors exist in this fixture. Keep its fault callback through
	// Store.Close so a concurrent query cannot outlive callback ownership.
	if err := f.engine.Store().DB().Callback().Query().Before("gorm:query").Register("cap_events_failure", func(db *gorm.DB) {
		if db.Statement.Table == "events" {
			_ = db.AddError(errors.New("private cap event failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	f.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/cap/state", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("unreadable durable events produced a successful observation")
	}
}

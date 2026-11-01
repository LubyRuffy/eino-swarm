package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func webExecutionEngine(t *testing.T) (*Engine, *atomic.Int32, *atomic.Int32) {
	return webExecutionEngineWithGate(t, nil)
}

func webExecutionEngineWithGate(t *testing.T, beforeTool func(context.Context)) (*Engine, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var cloud, local atomic.Int32
	const hostToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var origin string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/local-page":
			local.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("local page received account authority")
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><title>Local</title><body><article>Local execution evidence.</article></body></html>"))
			return
		case "/client/desktop/v1/web/fetch":
			cloud.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+hostToken {
				t.Error("cloud fetch did not use the account's Host credential")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"markdown":"Cloud execution evidence."}}`))
			return
		case "/chat/completions":
		default:
			t.Errorf("unexpected fixture route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(string(raw), hostToken) {
			t.Error("private Host credential reached the model")
		}
		var body struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
			Stream   bool             `json:"stream"`
		}
		if json.Unmarshal(raw, &body) != nil {
			t.Error("invalid model request")
			return
		}
		if len(body.Messages) > 0 {
			system, _ := body.Messages[0]["content"].(string)
			if replyNoScheduleClaimsFixture(t, w, system, body.Stream, len(body.Tools)) {
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, message := range body.Messages {
			if message["role"] == "tool" {
				_, _ = io.WriteString(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Web check completed.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				return
			}
		}
		if beforeTool != nil {
			beforeTool(r.Context())
		}
		args, err := json.Marshal(map[string]string{"url": origin + "/local-page"})
		if err != nil {
			t.Error(err)
			return
		}
		chunk := map[string]any{"id": "fetch", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "fetch-page", "type": "function", "function": map[string]any{"name": "web_fetch", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
	}))
	origin = endpoint.URL
	t.Cleanup(endpoint.Close)
	clearTestEngineSeed(t)
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "fixture"
	cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: origin, Model: "fixture"}}
	cfg.Swarm.AutoTitle, cfg.Memory.AutoReview = false, false
	cfg.Remote.AuthMode, cfg.Remote.HubURL = config.RemoteAuthNOSEC, origin
	path, err := cfg.StageRemoteToken(hostToken, origin, "public-test-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: hostToken, Origin: origin, Authorized: true, HostID: "host", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e, &cloud, &local
}

func TestAccountWebExecutionCrossesTheRealRegistryHTTPAndTrace(t *testing.T) {
	for _, preference := range []string{"", "local"} {
		t.Run("preference="+preference, func(t *testing.T) {
			e, cloud, local := webExecutionEngine(t)
			thread, err := e.CreateThread("Web execution fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadWebExecution(thread.ID, preference); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect the requested page")
			if err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			wantMode, wantEvidence := config.WebExecutionAuto, "Cloud execution evidence."
			if preference == config.WebExecutionLocal {
				wantMode, wantEvidence = config.WebExecutionLocal, "Local execution evidence."
			}
			if finished.Status != store.TurnDone || finished.WebExecution != wantMode || e.cfg.Snapshot().Tools.WebExecution != config.WebExecutionAuto {
				t.Fatalf("turn mode/global configuration changed: %+v", finished)
			}
			if preference == "" && (cloud.Load() != 1 || local.Load() != 0) || preference == "local" && (cloud.Load() != 0 || local.Load() != 1) {
				t.Fatalf("actual execution did not match the preference: cloud=%d local=%d", cloud.Load(), local.Load())
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events {
				if event.Kind == swarm.NotifyToolResult.String() && strings.Contains(event.Text, wantEvidence) {
					found = true
				}
			}
			if !found {
				t.Fatal("the selected Web execution result was absent from the turn Trace")
			}
		})
	}
}

func TestResumedTurnKeepsItsLocalExecutionSnapshotAfterConversationChanges(t *testing.T) {
	e, cloud, local := webExecutionEngine(t)
	thread, err := e.CreateThread("Restore local Web preference", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "Inspect the requested page")
	if err := e.store.DB().Model(&store.Turn{}).Where("id = ?", turn.ID).Update("web_execution", config.WebExecutionLocal).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadWebExecution(thread.ID, config.WebExecutionCloud); err != nil {
		t.Fatal(err)
	}
	if resumed, err := e.ResumeOrphanedTurns(); err != nil || resumed != 1 {
		t.Fatalf("resume=%d error=%v", resumed, err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || finished.WebExecution != config.WebExecutionLocal || cloud.Load() != 0 || local.Load() != 1 {
		t.Fatalf("restored local turn switched to cloud: turn=%+v cloud=%d local=%d", finished, cloud.Load(), local.Load())
	}
}

func TestRunningAutomaticTurnUsesTheNewlySignedInAccount(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	e, cloud, local := webExecutionEngineWithGate(t, func(ctx context.Context) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	if err := e.cfg.Update(func(next *config.Config) error { next.Remote.AuthMode = config.RemoteAuthLegacy; return nil }); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Sign in during an automatic turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the requested page")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model request never reached the account transition barrier")
	}
	if err := e.cfg.Update(func(next *config.Config) error { next.Remote.AuthMode = config.RemoteAuthNOSEC; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || cloud.Load() != 1 || local.Load() != 0 {
		t.Fatalf("running automatic turn kept a stale signed-out account: turn=%+v cloud=%d local=%d", finished, cloud.Load(), local.Load())
	}
}

func TestRunningCloudTurnStopsUsingTheOldGatewayAfterOriginChange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	e, oldCloud, local := webExecutionEngineWithGate(t, func(ctx context.Context) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	thread, err := e.CreateThread("Change gateway during a cloud turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadWebExecution(thread.ID, config.WebExecutionCloud); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the requested page")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model request never reached the origin transition barrier")
	}
	if err := e.cfg.Update(func(next *config.Config) error { next.Remote.HubURL = "https://new-gateway.invalid"; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || oldCloud.Load() != 0 || local.Load() != 0 {
		t.Fatalf("origin change kept using stale authority or local fallback: turn=%+v old_cloud=%d local=%d", finished, oldCloud.Load(), local.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || event.Kind == swarm.NotifyToolResult.String() && strings.Contains(event.Text, "sign in to the server again")
	}
	if !found {
		t.Fatal("origin change did not retain the reauthorization error in Trace")
	}
}

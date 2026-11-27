package server_test

import (
	"bytes"
	"encoding/json"
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
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGatewaySpeechHTTPRequiresOptInAndReleasesTheHeldUpstreamOnStop(t *testing.T) {
	first := []byte("event: speech.audio.delta\ndata: {\"type\":\"speech.audio.delta\",\"audio\":\"AQI=\"}\n\n")
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["model"] != "configured-voice" || body["response_format"] != "pcm" || body["stream_format"] != "sse" || r.Header.Get("Authorization") != "Bearer account-token" || r.URL.Path != "/client/desktop/v1/audio/speech" {
			t.Errorf("gateway speech request=%v decode=%v path=%q", body, err, r.URL.Path)
			http.Error(w, "gateway requires its incremental SSE protocol", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(first)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.Speech.Output.Format = "pcm"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pool := provider.New(cfg)
	source, err := provider.NewAccountSource("gateway:voice", "Gateway", upstream.URL+"/client/desktop/v1", "account-token", provider.Catalog{Names: []string{"configured-voice"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetRoles([]provider.AccountRole{{ID: "speech", Enabled: true}})
	source.SetModelPolicy(false, map[string]string{"speech": "configured-voice"})
	pool.SetAccountSource(source)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, st, pool, logger)
	defer eng.Shutdown()
	app, err := server.New(server.Options{Engine: eng, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	blocked, err := httpServer.Client().Post(httpServer.URL+"/api/audio/speech", "application/json", strings.NewReader(`{"text":"answer"}`))
	if err != nil {
		t.Fatal(err)
	}
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("fresh Gateway speech returned HTTP %d", blocked.StatusCode)
	}
	if err := cfg.Update(func(c *config.Config) error { c.Speech.Output.GatewayDisabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/audio/speech", strings.NewReader(`{"text":"answer"}`))
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got := make([]byte, len(first))
	if _, err := io.ReadFull(response.Body, got); err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" || !bytes.Equal(got, first) {
		t.Fatalf("held gateway first event=%q status=%d error=%v", got, response.StatusCode, err)
	}
	if release, ok := pool.TryHoldModelPolicy(); ok {
		release()
		t.Fatal("HTTP first event prematurely released model admission")
	}
	response.Body.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP Stop did not disconnect the held gateway stream")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if release, ok := pool.TryHoldModelPolicy(); ok {
			release()
			if err := cfg.Update(func(c *config.Config) error {
				c.Speech.Output.GatewayDisabled = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			response, err := client.Post(httpServer.URL+"/api/audio/speech", "application/json", strings.NewReader(`{"text":"answer"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("gateway opt-out returned HTTP %d", response.StatusCode)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("HTTP Stop did not release model admission")
}

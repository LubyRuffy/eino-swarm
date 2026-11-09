package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestModelRequestBudgetTracksAdmittedRequestsAcrossModels(t *testing.T) {
	for _, mode := range []string{"chat_completions", "responses"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			finishSlow := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				var limit, remaining string
				switch payload.Model {
				case "manager":
					close(entered)
					<-release
					limit, remaining = "100", "99"
				case "auxiliary":
					limit, remaining = "100", "83"
				case "invalid":
					limit, remaining = "100", "101"
				case "duplicate":
					limit, remaining = "100", "19"
					w.Header().Add("X-Zwai-Model-Requests-Remaining", "7")
				case "conflict":
					limit, remaining = "99", "5"
				case "exhausted":
					limit, remaining = "100", "0"
				case "reset":
					limit, remaining = "101", "100"
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Zwai-Model-Request-Limit", limit)
				w.Header().Add("X-Zwai-Model-Requests-Remaining", remaining)
				if r.URL.Path == "/v1/responses" {
					fmt.Fprint(w, responsesFixture)
					return
				}
				fmt.Fprintf(w, `{"id":"fixture","object":"chat.completion","model":%s,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`, strconv.Quote(payload.Model))
			}))
			defer server.Close()
			defer finishSlow()
			cfg := configFor(t, true)
			cfg.Models.Providers[0].BaseURL = server.URL + "/v1"
			cfg.Models.Providers[0].APIMode = mode
			cfg.Models.Providers = append(cfg.Models.Providers, config.Provider{ID: "other", BaseURL: server.URL + "/v1", APIKey: "different-route-key", Model: "auxiliary", APIMode: mode})
			pool := New(cfg)
			assertBudget := func(providerID string, wantLimit, wantRemaining int, wantOK bool) {
				t.Helper()
				limit, remaining, ok := pool.ModelRequestBudget(providerID)
				if limit != wantLimit || remaining != wantRemaining || ok != wantOK {
					t.Fatalf("provider %q budget = (%d, %d, %t), want (%d, %d, %t)", providerID, limit, remaining, ok, wantLimit, wantRemaining, wantOK)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			request := func(m model.BaseChatModel) error {
				_, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("status")})
				return err
			}
			get := func(name string) model.BaseChatModel {
				t.Helper()
				m, err := pool.Get(ctx, "", name)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			assertBudget("", 0, 0, false)
			manager := get("manager")
			done := make(chan error, 1)
			go func() { done <- request(manager) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("manager request never arrived")
			}
			aux, err := pool.AuxiliaryModelBuilder(ctx, "", "auxiliary", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := request(aux("reviewer", "reviewer")); err != nil {
				t.Fatal(err)
			}
			assertBudget("", 100, 83, true)
			assertBudget("other", 0, 0, false)
			finishSlow()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			assertBudget("", 100, 83, true)
			for _, name := range []string{"invalid", "duplicate", "conflict"} {
				if err := request(get(name)); err != nil {
					t.Fatal(err)
				}
				assertBudget("", 100, 83, true)
			}
			if err := request(get("exhausted")); err != nil {
				t.Fatal(err)
			}
			assertBudget("", 100, 0, true)
			pool.Invalidate()
			assertBudget("", 0, 0, false)
			if err := request(get("reset")); err != nil {
				t.Fatal(err)
			}
			assertBudget("", 101, 100, true)
		})
	}
}

func TestModelRequestBudgetIgnoresHeadersFromRedirectedOrigin(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zwai-Model-Request-Limit", "100")
		w.Header().Set("X-Zwai-Model-Requests-Remaining", "0")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer target.Close()
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer entry.Close()
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = entry.URL + "/v1"
	pool := New(cfg)
	m, err := pool.Get(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Generate(t.Context(), []*schema.Message{schema.UserMessage("status")})
	if err != nil || out.Content != "ok" {
		t.Fatalf("redirected provider call = %v, %v", out, err)
	}
	if limit, remaining, ok := pool.ModelRequestBudget(""); ok {
		t.Fatalf("accepted budget from a different origin = %d/%d", remaining, limit)
	}
}

func TestModelRequestBudgetSurvivesInterruptedStreamWithoutUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Zwai-Model-Request-Limit", "100")
		w.Header().Set("X-Zwai-Model-Requests-Remaining", "4")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"fixture","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = server.URL + "/v1"
	pool := New(cfg)
	m, err := pool.Get(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.Stream(t.Context(), []*schema.Message{schema.UserMessage("status")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for err == nil {
		_, err = stream.Recv()
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("stream ended with %v, want interrupted stream", err)
	}
	if limit, remaining, ok := pool.ModelRequestBudget(""); !ok || limit != 100 || remaining != 4 {
		t.Fatalf("admitted but interrupted stream budget = (%d, %d, %t)", limit, remaining, ok)
	}
}

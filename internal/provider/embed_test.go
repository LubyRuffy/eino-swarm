package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNgramEmbedRanksOverlappingTextAboveUnrelated(t *testing.T) {
	q := NgramEmbed([]string{"alpha beta gamma"})[0]
	near := NgramEmbed([]string{"alpha beta"})[0]
	far := NgramEmbed([]string{"zzzzzzzz"})[0]
	if store.Cosine(q, near) <= store.Cosine(q, far) {
		t.Fatalf("overlapping n-grams must score higher: near=%v far=%v", store.Cosine(q, near), store.Cosine(q, far))
	}
}

func TestMockPoolEmbedNeverHitsTheNetwork(t *testing.T) {
	cfg := configFor(t, true)
	p := NewMock(cfg)
	got, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0]) != ngramDim {
		t.Fatalf("mock embed shape: %d x %d", len(got), len(got[0]))
	}
}

func TestEmbedPostsOpenAICompatibleBody(t *testing.T) {
	var sawPath, sawAuth, sawModel string
	var sawInput []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		var body embedRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body: %v", err)
		}
		sawModel = body.Model
		sawInput = body.Input
		_ = json.NewEncoder(w).Encode(embedResponse{Data: []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}{
			{Index: 1, Embedding: []float32{0, 1}},
			{Index: 0, Embedding: []float32{1, 0}},
		}})
	}))
	t.Cleanup(srv.Close)

	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	cfg.Models.Providers[0].APIKey = "secret"
	p := New(cfg)
	got, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if sawPath != "/v1/embeddings" {
		t.Fatalf("path=%q", sawPath)
	}
	if sawAuth != "Bearer secret" {
		t.Fatalf("auth=%q", sawAuth)
	}
	if sawModel != "named-embed" {
		t.Fatalf("model=%q (must be the pin, not a baked-in name)", sawModel)
	}
	if strings.Join(sawInput, ",") != "alpha,beta" {
		t.Fatalf("input=%v", sawInput)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][1] != 1 {
		t.Fatalf("vectors must follow index, got %+v", got)
	}
}

func TestEmbedRequiresAModelNameAndAURL(t *testing.T) {
	cfg := configFor(t, true)
	p := New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "", []string{"alpha"}); err == nil {
		t.Fatal("empty model must fail")
	}
	cfg.Models.Providers[0].BaseURL = ""
	p = New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"}); err == nil {
		t.Fatal("empty URL must fail")
	}
}

func TestParseEmbeddingsRejectsAShortPayload(t *testing.T) {
	_, err := parseEmbeddings([]byte(`{"data":[{"index":0,"embedding":[1]}]}`), 2)
	if err == nil {
		t.Fatal("short payload must fail")
	}
	_, err = parseEmbeddings([]byte(`{"data":[{"index":0,"embedding":[]}]}`), 1)
	if err == nil {
		t.Fatal("empty vector must fail")
	}
	_, err = parseEmbeddings([]byte(`{"data":[{"index":3,"embedding":[1]}]}`), 1)
	if err == nil {
		t.Fatal("out of range index must fail")
	}
	_, err = parseEmbeddings([]byte(`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`), 2)
	if err == nil {
		t.Fatal("repeated index must fail")
	}
	_, err = parseEmbeddings([]byte(`{"data":[{"index":1,"embedding":[1]}]}`), 1)
	if err == nil {
		t.Fatal("missing index 0 must fail")
	}
}

func TestEmbedReportsHTTPFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	t.Cleanup(srv.Close)
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	p := New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"}); err == nil {
		t.Fatal("a 502 must fail")
	}
}

func TestNgramEmbedEmptyTextIsAZeroVector(t *testing.T) {
	got := NgramEmbed([]string{""})
	if len(got) != 1 || len(got[0]) != ngramDim {
		t.Fatalf("shape %d x %d", len(got), len(got[0]))
	}
	for _, x := range got[0] {
		if x != 0 {
			t.Fatalf("empty text must be zeros, got %v", x)
		}
	}
}

func TestEmbedFailsWhenTheEndpointIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = base + "/v1"
	p := New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"}); err == nil {
		t.Fatal("a dead endpoint must fail")
	}
}

func TestEmbedRejectsAnOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxEmbedBytes+2))
	}))
	t.Cleanup(srv.Close)
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	p := New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"}); err == nil {
		t.Fatal("an oversized body must fail")
	}
}

func TestEmbedRejectsANonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(srv.Close)
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	p := New(cfg)
	if _, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", []string{"alpha"}); err == nil {
		t.Fatal("junk JSON must fail")
	}
}

func TestEmbedEmptyInputIsANoop(t *testing.T) {
	p := New(configFor(t, true))
	got, err := p.Embed(context.Background(), "default", "named-embed", nil)
	if err != nil || got != nil {
		t.Fatalf("empty input: %v %+v", err, got)
	}
}

func TestEmbedSplitsALongBatch(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var body embedRequest
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body: %v", err)
			return
		}
		data := make([]struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}, len(body.Input))
		for i := range body.Input {
			data[i].Index = i
			data[i].Embedding = []float32{1}
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Data: data})
	}))
	t.Cleanup(srv.Close)
	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	p := New(cfg)
	texts := make([]string, embedBatch+1)
	for i := range texts {
		texts[i] = "alpha"
	}
	got, err := p.Embed(context.Background(), cfg.Models.Default, "named-embed", texts)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want 2 batches, got %d", calls)
	}
	if len(got) != len(texts) {
		t.Fatalf("len=%d", len(got))
	}
}

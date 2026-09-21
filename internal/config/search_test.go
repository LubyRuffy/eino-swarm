package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchEmbeddingDefaultsOffAndNamesNothing(t *testing.T) {
	cfg := Default()
	if cfg.Search.Embedding {
		t.Fatal("semantic search must ship off")
	}
	if cfg.Search.EmbeddingProvider != "" || cfg.Search.EmbeddingModel != "" {
		t.Fatalf("an embedding pin must not be baked in: %+v", cfg.Search)
	}
	if cfg.Search.SemanticEnabled() {
		t.Fatal("off + empty model is not semantic search")
	}

	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	written, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	raw, err := os.ReadFile(written.Path())
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(raw))
	for _, leak := range []string{"text-embedding", "ada", "bge-", "nomic"} {
		if strings.Contains(body, leak) {
			t.Fatalf("default config leaked an embedding product name %q:\n%s", leak, raw)
		}
	}
}

func TestSearchEmbeddingOffSurvivesNormalize(t *testing.T) {
	dir := t.TempDir()
	raw := "search:\n  embedding: false\n  embedding_model: leftover\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Search.Embedding {
		t.Fatal("normalize turned embedding back on")
	}
	if cfg.Search.EmbeddingModel != "leftover" {
		t.Fatalf("a typed model must survive while the switch is off: %+v", cfg.Search)
	}
	if cfg.Search.SemanticEnabled() {
		t.Fatal("a leftover model with the switch off must not enable embeddings")
	}
}

func TestSearchEmbeddingOnWithoutModelIsKeywordOnly(t *testing.T) {
	s := SearchConfig{Embedding: true, EmbeddingModel: "  "}
	if s.SemanticEnabled() {
		t.Fatal("whitespace is not a model name")
	}
	s.EmbeddingModel = "named-embed"
	if !s.SemanticEnabled() {
		t.Fatal("switch + model must enable semantic search")
	}
}

func TestUnknownEmbeddingProviderIsCleared(t *testing.T) {
	dir := t.TempDir()
	raw := "search:\n  embedding: true\n  embedding_provider: gone\n  embedding_model: named-embed\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Search.EmbeddingProvider != "" {
		t.Fatalf("deleted provider kept: %+v", cfg.Search)
	}
	if !cfg.Search.Embedding || cfg.Search.EmbeddingModel != "named-embed" {
		t.Fatalf("the switch and model must survive a ghost provider: %+v", cfg.Search)
	}
	if got := cfg.Search.ResolveProvider(cfg.Models.Default); got != cfg.Models.Default {
		t.Fatalf("empty provider must follow default, got %q", got)
	}
	s := SearchConfig{EmbeddingProvider: "local"}
	if got := s.ResolveProvider("default"); got != "local" {
		t.Fatalf("a pin must win, got %q", got)
	}
}

func TestSearchSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	next := Default()
	next.Models.Providers = []Provider{{
		ID: "local", Label: "Local", BaseURL: "http://local.invalid/v1",
		Model: "chat", TimeoutSeconds: 30,
	}}
	next.Models.Default = "local"
	next.Search.Embedding = true
	next.Search.EmbeddingProvider = "local"
	next.Search.EmbeddingModel = "named-embed"
	if err := cfg.Replace(next); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Search.Embedding || reloaded.Search.EmbeddingProvider != "local" ||
		reloaded.Search.EmbeddingModel != "named-embed" {
		t.Fatalf("search pin did not survive: %+v", reloaded.Search)
	}
	if !reloaded.Search.SemanticEnabled() {
		t.Fatal("round-tripped pin must be semantically enabled")
	}
}

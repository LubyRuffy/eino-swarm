package config

import "strings"

// SearchConfig is conversation search. Keyword indexing (FTS5) is always on.
// Semantic search is opt-in: a missing or false embedding switch must stay
// off, or a Settings toggle would turn itself back on behind the user's back.
type SearchConfig struct {
	// Embedding turns on vector search against an embedding model. Default
	// off: calling an extra endpoint is a cost the install did not ask for.
	Embedding bool `yaml:"embedding" json:"embedding"`
	// EmbeddingProvider is which endpoint /embeddings is sent to. Empty
	// follows models.default. A deleted id is cleared on load.
	EmbeddingProvider string `yaml:"embedding_provider" json:"embedding_provider"`
	// EmbeddingModel is the name that endpoint expects for embeddings.
	// Required when Embedding is true; empty means keyword search only.
	// Never defaulted to a baked-in name.
	EmbeddingModel string `yaml:"embedding_model" json:"embedding_model"`
}

// SemanticEnabled is true only when the user both flipped the switch and
// named a model. A half-filled row must not start calling /embeddings.
func (s SearchConfig) SemanticEnabled() bool {
	return s.Embedding && strings.TrimSpace(s.EmbeddingModel) != ""
}

// ResolveProvider is the endpoint id embeddings use. Empty follows the
// configured default, the same way the namer does.
func (s SearchConfig) ResolveProvider(defaultID string) string {
	if p := strings.TrimSpace(s.EmbeddingProvider); p != "" {
		return p
	}
	return strings.TrimSpace(defaultID)
}

func (c *Config) normalizeSearch() {
	c.Search.EmbeddingProvider = strings.TrimSpace(c.Search.EmbeddingProvider)
	c.Search.EmbeddingModel = strings.TrimSpace(c.Search.EmbeddingModel)
	if c.Search.EmbeddingProvider != "" && c.providerIndex(c.Search.EmbeddingProvider) < 0 {
		// A deleted endpoint must not keep embedding against a ghost id.
		c.Search.EmbeddingProvider = ""
	}
}

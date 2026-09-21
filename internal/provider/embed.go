package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const (
	embeddingsPath = "/embeddings"
	maxEmbedBytes  = 8 << 20
	embedBatch     = 32
	ngramDim       = 256
)

// Embed turns texts into vectors on the named embedding model. The scripted
// pool never leaves the process: it hashes character n-grams so --mock and
// the tests still exercise semantic ranking.
func (p *Pool) Embed(ctx context.Context, providerID, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if p.mock {
		return NgramEmbed(texts), nil
	}
	prov, err := p.Resolve(providerID)
	if err != nil {
		return nil, err
	}
	if !prov.EndpointReady() {
		return nil, fmt.Errorf("provider: %q has no base_url; open Settings and finish setting it up", prov.ID)
	}
	name := strings.TrimSpace(model)
	if name == "" {
		return nil, fmt.Errorf("provider: an embedding model name is required")
	}
	return fetchEmbeddings(ctx, prov, name, texts)
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func fetchEmbeddings(ctx context.Context, p config.Provider, model string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embedBatch {
		end := start + embedBatch
		if end > len(texts) {
			end = len(texts)
		}
		part, err := fetchEmbeddingBatch(ctx, p, model, texts[start:end])
		if err != nil {
			return nil, err
		}
		copy(out[start:], part)
	}
	return out, nil
}

func fetchEmbeddingBatch(ctx context.Context, p config.Provider, model string, texts []string) ([][]float32, error) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	body, err := json.Marshal(embedRequest{Model: model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("provider: encode embeddings: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+embeddingsPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider: embeddings: %w", err)
	}
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: p.Timeout()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider: embeddings: %w%s", err, localNetworkHint(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxEmbedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("provider: embeddings: %w", err)
	}
	if len(raw) > maxEmbedBytes {
		return nil, fmt.Errorf("provider: embedding response was larger than 8MB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider: embeddings failed (%s)", resp.Status)
	}
	return parseEmbeddings(raw, len(texts))
}

func parseEmbeddings(raw []byte, n int) ([][]float32, error) {
	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("provider: embeddings: %w", err)
	}
	if len(parsed.Data) != n {
		return nil, fmt.Errorf("provider: embeddings returned %d vectors for %d inputs", len(parsed.Data), n)
	}
	out := make([][]float32, n)
	seen := map[int]bool{}
	for _, row := range parsed.Data {
		if row.Index < 0 || row.Index >= n {
			return nil, fmt.Errorf("provider: embeddings index %d out of range", row.Index)
		}
		if seen[row.Index] {
			return nil, fmt.Errorf("provider: embeddings repeated index %d", row.Index)
		}
		if len(row.Embedding) == 0 {
			return nil, fmt.Errorf("provider: embeddings index %d was empty", row.Index)
		}
		seen[row.Index] = true
		out[row.Index] = row.Embedding
	}
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("provider: embeddings missing index %d", i)
		}
	}
	return out, nil
}

// NgramEmbed is a local stand-in for an embedding model. Nearby strings share
// character n-grams; it is not a substitute for a real pin, only for --mock.
func NgramEmbed(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = ngramVector(text)
	}
	return out
}

func ngramVector(text string) []float32 {
	v := make([]float32, ngramDim)
	runes := []rune(strings.ToLower(text))
	if len(runes) == 0 {
		return v
	}
	for n := 1; n <= 3; n++ {
		if len(runes) < n {
			break
		}
		for i := 0; i+n <= len(runes); i++ {
			h := fnv.New32a()
			_, _ = h.Write([]byte(string(runes[i : i+n])))
			v[int(h.Sum32())%ngramDim]++
		}
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= scale
	}
	return v
}

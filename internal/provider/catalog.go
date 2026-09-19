package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const (
	maxCatalogBytes  = 1 << 20
	modelsPath       = "/models"
	maxCatalogWindow = 1_000_000_000
)

// Catalog is what an endpoint listed: names in the order they arrived, plus
// any context windows the body happened to include. Most OpenAI-compatible
// /models responses have no window; those leave Windows empty rather than
// inventing one from the name.
type Catalog struct {
	Names   []string
	Windows map[string]int
}

// Discover lists the models an endpoint serves. The result is not saved:
// Settings holds it until the user picks a default and clicks Save.
// Offline pools never hit the network.
func (p *Pool) Discover(ctx context.Context, prov config.Provider) (Catalog, error) {
	if p.mock {
		names := prov.Models()
		if len(names) == 0 {
			names = []string{MockModelName}
		}
		windows := make(map[string]int, len(names))
		for _, n := range names {
			windows[n] = MockContextWindow
		}
		return Catalog{Names: names, Windows: windows}, nil
	}
	if p.discover != nil {
		return p.discover(ctx, prov)
	}
	return fetchOpenAICatalog(ctx, prov)
}

func fetchOpenAICatalog(ctx context.Context, p config.Provider) (Catalog, error) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return Catalog{}, fmt.Errorf("provider: a base URL is required to list models")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+modelsPath, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("provider: list models: %w", err)
	}
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: discoverTimeout(p)}
	resp, err := client.Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("provider: list models: %w%s", err, localNetworkHint(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("provider: list models: %w", err)
	}
	if len(body) > maxCatalogBytes {
		return Catalog{}, fmt.Errorf("provider: model list from the endpoint was larger than 1MB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Catalog{}, fmt.Errorf("provider: listing models failed (%s)", resp.Status)
	}
	cat, err := parseModelCatalog(body)
	if err != nil {
		return Catalog{}, err
	}
	if len(cat.Names) == 0 {
		return Catalog{}, fmt.Errorf("provider: the endpoint listed no models")
	}
	return cat, nil
}

func discoverTimeout(p config.Provider) time.Duration {
	t := p.Timeout()
	if t <= 0 || t > config.DefaultDiscoverTimeout {
		return config.DefaultDiscoverTimeout
	}
	return t
}

// parseModelCatalog reads an OpenAI-compatible /models body. The id is the
// name; a handful of common window keys are copied when present, including
// one level of nested objects some vendors wrap them in. Extra keys are
// ignored so a vendor's extra metadata cannot break discovery.
func parseModelCatalog(raw []byte) (Catalog, error) {
	var list struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return Catalog{}, fmt.Errorf("provider: could not parse the model list: %w", err)
	}
	cat := Catalog{Names: make([]string, 0, len(list.Data))}
	seen := map[string]bool{}
	for _, row := range list.Data {
		id, window := catalogRow(row)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cat.Names = append(cat.Names, id)
		if window > 0 {
			if cat.Windows == nil {
				cat.Windows = map[string]int{}
			}
			cat.Windows[id] = window
		}
	}
	return cat, nil
}

func catalogRow(raw json.RawMessage) (string, int) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", 0
	}
	id := strings.TrimSpace(head.ID)
	if id == "" {
		return "", 0
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return id, 0
	}
	return id, windowFromObject(obj, 0)
}

// Keys we will copy as a context window. max_tokens / max_completion_tokens
// stay out: those are output caps, and treating them as the window would
// paint a 4k ring on a 128k model.
var catalogWindowKeys = []string{
	"context_length",
	"max_model_len",
	"context_window",
	"max_context_length",
	"max_input_tokens",
	"n_ctx",
	"max_seq_len",
}

var catalogWindowParents = []string{
	"top_provider",
	"meta",
	"limits",
	"parameters",
}

func windowFromObject(obj map[string]json.RawMessage, depth int) int {
	for _, key := range catalogWindowKeys {
		if n := jsonPositiveInt(obj[key]); n > 0 {
			return n
		}
	}
	if depth >= 1 {
		return 0
	}
	for _, parent := range catalogWindowParents {
		raw, ok := obj[parent]
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) != nil {
			continue
		}
		if n := windowFromObject(nested, depth+1); n > 0 {
			return n
		}
	}
	return 0
}

func jsonPositiveInt(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	if n <= 0 || n > maxCatalogWindow {
		return 0
	}
	return int(n)
}

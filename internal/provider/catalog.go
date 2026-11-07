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
// inventing one from the name. Types holds a non-empty model_type when the
// server sent one. A missing entry is unclassified, not a guess from the name.
type Catalog struct {
	Names   []string
	Windows map[string]int
	Types   map[string]string
}

// Conversational keeps chat rows and rows the server never classified.
// Embedding, OCR, decisions and any other non-empty type stay out of a
// conversation picker. The original catalog keeps them for the surfaces
// that ask for a type.
func (c Catalog) Conversational() Catalog {
	out := Catalog{}
	for _, name := range c.Names {
		if !conversationalModelType(c.Types[name]) {
			continue
		}
		out.Names = append(out.Names, name)
		if n := c.Windows[name]; n > 0 {
			if out.Windows == nil {
				out.Windows = map[string]int{}
			}
			out.Windows[name] = n
		}
	}
	if out.Names == nil {
		out.Names = []string{}
	}
	return out
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
	// A provider's saved catalog is the conversation list. Typed embedding,
	// OCR and decisions rows stay out of that picker. The account projection
	// keeps the full parse so Settings can count types and pin an embedding.
	cat = cat.Conversational()
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
// one level of nested objects some vendors wrap them in. model_type, when
// present, decides whether the name can be offered for a conversation.
// Other extra keys are ignored so a vendor's metadata cannot break discovery.
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
		id, window, kind := catalogRow(row)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cat.Names = append(cat.Names, id)
		// The first classification wins. A repeated id does not change it.
		if kind != "" {
			if cat.Types == nil {
				cat.Types = map[string]string{}
			}
			cat.Types[id] = kind
		}
		if window > 0 {
			if cat.Windows == nil {
				cat.Windows = map[string]int{}
			}
			cat.Windows[id] = window
		}
	}
	return cat, nil
}

func catalogRow(raw json.RawMessage) (string, int, string) {
	var head struct {
		ID        string `json:"id"`
		ModelType string `json:"model_type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", 0, ""
	}
	id := strings.TrimSpace(head.ID)
	if id == "" {
		return "", 0, ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return id, 0, strings.TrimSpace(head.ModelType)
	}
	return id, windowFromObject(obj, 0), strings.TrimSpace(head.ModelType)
}

// conversationalModelType is the server's admin classification for a catalog
// row. chat and an omitted type can be selected for a conversation. Any other
// value, including embedding, ocr and decisions, cannot.
func conversationalModelType(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "", "chat":
		return true
	default:
		return false
	}
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

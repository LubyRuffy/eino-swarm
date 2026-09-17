// Package provider turns configured endpoints into eino chat models. It owns
// one client per provider so a whole swarm shares a single connection pool,
// records per-call telemetry for tracing, and ships a scripted offline
// provider that lets the UI and its tests run with no endpoint at all.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// CallRecord is one model request, as the trace view reports it.
type CallRecord struct {
	AgentID          string
	ProviderID       string
	Model            string
	InputMsgs        int
	InputChars       int
	OutputChars      int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	Duration         time.Duration
	Err              error
}

// Recorder receives one CallRecord per model request. Implementations must be
// safe for concurrent use: a swarm calls several models at once.
type Recorder func(CallRecord)

// Info describes one selectable model to the UI. A provider with a catalog
// of N names produces N rows so the composer can switch without a Settings
// row per name.
type Info struct {
	ID            string `json:"id"`
	ProviderID    string `json:"provider_id"`
	ProviderLabel string `json:"provider_label"`
	Label         string `json:"label"`
	Model         string `json:"model"`
	Ready         bool   `json:"ready"`
	Default       bool   `json:"default"`
	// ContextWindow is this name's token limit, or 0 when nobody has said.
	ContextWindow int `json:"context_window"`
}

// ChoiceID is the composer's selection key: provider plus model. A tab
// cannot appear in a provider id we generate, and is illegal in most model
// names; the UI treats it as opaque.
func ChoiceID(providerID, model string) string {
	return providerID + "\t" + model
}

// builder constructs the underlying model for one provider. It is swapped out
// by NewMockPool so the whole stack can run offline.
type builder func(ctx context.Context, p config.Provider) (model.BaseChatModel, error)

// Pool hands out chat models by provider id, caching one instance each.
//
// Caching matters beyond speed: a swarm can have a dozen agents talking to the
// same endpoint at once, and one shared client means one shared connection
// pool instead of a dozen.
type Pool struct {
	cfg   *config.Config
	build builder
	mock  bool

	mu    sync.Mutex
	cache map[string]model.BaseChatModel
	// discover is swapped in tests so listing models does not need a network.
	discover func(ctx context.Context, p config.Provider) (Catalog, error)
}

// New returns a pool backed by real OpenAI-compatible endpoints.
func New(cfg *config.Config) *Pool {
	return &Pool{cfg: cfg, build: buildOpenAI, cache: map[string]model.BaseChatModel{}}
}

// NewMock returns a pool whose models are scripted: no network, deterministic
// output, and a full manager/worker swarm exercise. This is what `--mock`
// runs on and what the end-to-end tests drive.
func NewMock(cfg *config.Config) *Pool {
	return &Pool{
		cfg:   cfg,
		build: func(context.Context, config.Provider) (model.BaseChatModel, error) { return newMockModel(""), nil },
		mock:  true,
		cache: map[string]model.BaseChatModel{},
	}
}

// IsMock reports whether this pool is the scripted offline one, which the UI
// surfaces so nobody mistakes a demo answer for a real one.
func (p *Pool) IsMock() bool { return p.mock }

// Resolve returns the provider record for id, falling back to the configured
// default when id is empty.
func (p *Pool) Resolve(id string) (config.Provider, error) {
	prov, ok := p.cfg.Provider(id)
	if !ok {
		return config.Provider{}, fmt.Errorf("provider: unknown provider %q", id)
	}
	if p.mock {
		// Offline runs are usually against an unconfigured provider, and a
		// turn recorded with a blank model name is unreadable in a trace.
		// The label is overwritten too: nothing here reaches the configured
		// endpoint, so naming it after that endpoint would be a lie. A
		// conversation that already picked a catalog name keeps it, so the
		// composer switch is what the trace shows.
		if strings.TrimSpace(prov.Model) == "" {
			prov.Model = MockModelName
		}
		prov.Label = "Offline (scripted)"
	}
	return prov, nil
}

// ResolveModel is Resolve plus a per-conversation override. Empty model
// means the provider's configured default (and, offline, MockModelName).
func (p *Pool) ResolveModel(id, model string) (config.Provider, error) {
	prov, err := p.Resolve(id)
	if err != nil {
		return config.Provider{}, err
	}
	if m := strings.TrimSpace(model); m != "" {
		prov.Model = m
	}
	return prov, nil
}

// WindowFor is the token limit the composer meter uses for this selection.
// The scripted provider invents MockContextWindow when nothing is configured,
// so --mock still has a ring; a real endpoint stays at 0 until Settings or
// Discover fills one in.
func (p *Pool) WindowFor(id, model string) int {
	prov, err := p.ResolveModel(id, model)
	if err != nil {
		if p.mock {
			return MockContextWindow
		}
		return 0
	}
	if n := prov.WindowFor(model); n > 0 {
		return n
	}
	if p.mock {
		return MockContextWindow
	}
	return 0
}

// MockModelName is what the scripted provider reports as its model, so a
// stored turn always says what produced it.
const MockModelName = "mock"

// MockContextWindow is the scripted provider's simulated limit. Real
// endpoints never inherit it: it exists so --mock still has a ring to show.
const MockContextWindow = 128000

// List describes every selectable model for the composer. One provider with
// a catalog of three names is three rows, grouped by provider_id in the UI.
func (p *Pool) List() []Info {
	out := make([]Info, 0, len(p.cfg.Models.Providers))
	for _, prov := range p.cfg.Models.Providers {
		if resolved, err := p.Resolve(prov.ID); err == nil {
			prov = resolved
		}
		names := prov.Models()
		if len(names) == 0 && p.mock {
			names = []string{MockModelName}
		}
		ready := p.mock || prov.EndpointReady()
		label := prov.GroupName()
		for _, name := range names {
			out = append(out, Info{
				ID:            ChoiceID(prov.ID, name),
				ProviderID:    prov.ID,
				ProviderLabel: label,
				Label:         name,
				Model:         name,
				Ready:         ready && name != "",
				Default:       name == prov.Model || (prov.Model == "" && p.mock && name == MockModelName),
				ContextWindow: p.WindowFor(prov.ID, name),
			})
		}
	}
	return out
}

func cacheKey(id, model string) string {
	return id + "\x00" + strings.TrimSpace(model)
}

// Get returns the shared chat model for a provider id and model name.
func (p *Pool) Get(ctx context.Context, id, model string) (model.BaseChatModel, error) {
	prov, err := p.ResolveModel(id, model)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := cacheKey(prov.ID, prov.Model)
	if m, ok := p.cache[key]; ok {
		return m, nil
	}
	m, err := p.build(ctx, prov)
	if err != nil {
		return nil, err
	}
	p.cache[key] = m
	return m, nil
}

// Invalidate drops cached clients. Settings changes call it so the next turn
// picks up the new endpoint without a restart.
func (p *Pool) Invalidate() {
	p.mu.Lock()
	p.cache = map[string]model.BaseChatModel{}
	p.mu.Unlock()
}

// ModelBuilder returns the per-agent model factory the swarm registry needs.
// Every agent in a run shares the provider's client for a given model name
// and gets its own telemetry-recording wrapper so a trace can attribute each
// call to an agent. effort is the conversation's thinking level, applied to
// every model call as an option so the shared client stays cached: an empty
// effort sends nothing. model overrides the provider's configured default.
//
// The mock provider is the exception: it scripts each role differently, so it
// builds a fresh model per agent.
func (p *Pool) ModelBuilder(ctx context.Context, id, modelName, effort string, rec Recorder) (func(role, agentID string) model.BaseChatModel, error) {
	prov, err := p.ResolveModel(id, modelName)
	if err != nil {
		return nil, err
	}
	effort = config.NormalizeReasoning(effort)
	if p.mock {
		return func(role, agentID string) model.BaseChatModel {
			return wrap(newMockModel(role), agentID, prov, effort, rec)
		}, nil
	}
	if !prov.Ready() {
		return nil, fmt.Errorf("provider: %q has no base_url or model configured; open Settings and finish setting it up", prov.ID)
	}

	shared, err := p.Get(ctx, prov.ID, prov.Model)
	if err != nil {
		return nil, err
	}
	return func(role, agentID string) model.BaseChatModel {
		return wrap(shared, agentID, prov, effort, rec)
	}, nil
}

func buildOpenAI(ctx context.Context, p config.Provider) (model.BaseChatModel, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("provider: %q needs a base_url and a model", p.ID)
	}
	// HTTPClient, not Timeout: eino puts Timeout on http.Client, which
	// includes the streamed body and kills a long thought mid-token.
	m, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL:    strings.TrimSpace(p.BaseURL),
		APIKey:     strings.TrimSpace(p.APIKey),
		Model:      strings.TrimSpace(p.Model),
		HTTPClient: chatHTTPClient(p.Timeout()),
	})
	if err != nil {
		return nil, fmt.Errorf("provider: build %q: %w", p.ID, err)
	}
	// eino-ext copies Message.Content onto ChatCompletionMessage even when
	// UserInputMultiContent is set; go-openai then refuses to marshal.
	return &exclusiveContentModel{inner: m}, nil
}

// ---------- telemetry ----------

// wrap returns m instrumented to report every call to rec and to carry the
// conversation's thinking level. A model with neither a recorder nor an effort
// is returned untouched, so there is no cost when nobody is watching and no
// reasoning field is sent to endpoints that never asked for one.
func wrap(m model.BaseChatModel, agentID string, p config.Provider, effort string, rec Recorder) model.BaseChatModel {
	if rec == nil && effort == "" {
		return m
	}
	return &recordingModel{inner: m, agentID: agentID, providerID: p.ID, model: p.Model, reasoning: effort, rec: rec}
}

// recordingModel times each request and reports its shape. It deliberately
// records sizes rather than prompts: enough to explain a slow or failed turn
// without copying whole conversations into the database. It also injects the
// conversation's reasoning effort as a per-call option, which keeps the shared
// client cached instead of one instance per thinking level.
type recordingModel struct {
	inner      model.BaseChatModel
	agentID    string
	providerID string
	model      string
	reasoning  string
	rec        Recorder
}

// withReasoning prepends the reasoning-effort option when one is set. It goes
// first so an explicit caller option later in the list still wins.
func (r *recordingModel) withReasoning(opts []model.Option) []model.Option {
	if r.reasoning == "" {
		return opts
	}
	effort := openaimodel.WithReasoningEffort(openaimodel.ReasoningEffortLevel(r.reasoning))
	return append([]model.Option{effort}, opts...)
}

func (r *recordingModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	opts = r.withReasoning(opts)
	start := time.Now()
	out, err := r.inner.Generate(ctx, in, opts...)
	if r.rec == nil {
		return out, err
	}
	rec := r.base(in, start)
	rec.Err = err
	if out != nil {
		rec.OutputChars = len(out.Content) + len(out.ReasoningContent)
		applyUsage(&rec, tokenUsageOf(out))
	}
	r.rec(rec)
	return out, err
}

func (r *recordingModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	opts = r.withReasoning(opts)
	start := time.Now()
	stream, err := r.inner.Stream(ctx, in, opts...)
	if r.rec == nil {
		return stream, err
	}
	if err != nil {
		rec := r.base(in, start)
		rec.Err = err
		r.rec(rec)
		return nil, err
	}
	// A streamed call is not over when Stream returns, it is over when the
	// stream is drained — so relay it and emit the record at the end, where
	// both the real duration and the output size are known. Relaying through a
	// pipe also means an abandoned stream cannot leak the goroutine: Send
	// reports the closed reader and the loop stops.
	sr, sw := schema.Pipe[*schema.Message](streamRelayBuffer)
	go func() {
		defer stream.Close()
		defer sw.Close()
		chars := 0
		var usage *schema.TokenUsage
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				rec := r.base(in, start)
				rec.OutputChars = chars
				applyUsage(&rec, usage)
				if !isStreamEnd(recvErr) {
					rec.Err = recvErr
					sw.Send(nil, recvErr)
				}
				r.rec(rec)
				return
			}
			if msg != nil {
				chars += len(msg.Content) + len(msg.ReasoningContent)
				usage = mergeUsage(usage, msg)
			}
			if closed := sw.Send(msg, nil); closed {
				rec := r.base(in, start)
				rec.OutputChars = chars
				applyUsage(&rec, usage)
				r.rec(rec)
				return
			}
		}
	}()
	return sr, nil
}

// streamRelayBuffer keeps the relay from serializing the provider's stream
// behind the consumer's rendering work.
const streamRelayBuffer = 16

func isStreamEnd(err error) bool {
	return errors.Is(err, io.EOF)
}

func (r *recordingModel) base(in []*schema.Message, start time.Time) CallRecord {
	chars := 0
	for _, m := range in {
		if m != nil {
			chars += len(m.Content)
		}
	}
	return CallRecord{
		AgentID:    r.agentID,
		ProviderID: r.providerID,
		Model:      r.model,
		InputMsgs:  len(in),
		InputChars: chars,
		Duration:   time.Since(start),
	}
}

func tokenUsageOf(msg *schema.Message) *schema.TokenUsage {
	if msg == nil || msg.ResponseMeta == nil {
		return nil
	}
	return msg.ResponseMeta.Usage
}

func mergeUsage(cur *schema.TokenUsage, msg *schema.Message) *schema.TokenUsage {
	next := tokenUsageOf(msg)
	if next == nil || (next.PromptTokens == 0 && next.CompletionTokens == 0 && next.TotalTokens == 0) {
		return cur
	}
	return next
}

func applyUsage(rec *CallRecord, u *schema.TokenUsage) {
	if rec == nil || u == nil {
		return
	}
	rec.PromptTokens = u.PromptTokens
	rec.CompletionTokens = u.CompletionTokens
	rec.TotalTokens = u.TotalTokens
	rec.CachedTokens = u.PromptTokenDetails.CachedTokens
	rec.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	if rec.TotalTokens == 0 {
		rec.TotalTokens = rec.PromptTokens + rec.CompletionTokens
	}
}

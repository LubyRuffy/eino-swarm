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
	AgentID     string
	ProviderID  string
	Model       string
	InputMsgs   int
	InputChars  int
	OutputChars int
	Duration    time.Duration
	Err         error
}

// Recorder receives one CallRecord per model request. Implementations must be
// safe for concurrent use: a swarm calls several models at once.
type Recorder func(CallRecord)

// Info describes a provider to the UI.
type Info struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Model string `json:"model"`
	Ready bool   `json:"ready"`
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
	return prov, nil
}

// List describes every configured provider for the settings UI.
func (p *Pool) List() []Info {
	out := make([]Info, 0, len(p.cfg.Models.Providers))
	for _, prov := range p.cfg.Models.Providers {
		out = append(out, Info{
			ID:    prov.ID,
			Label: prov.DisplayName(),
			Model: prov.Model,
			Ready: p.mock || prov.Ready(),
		})
	}
	return out
}

// Get returns the shared chat model for a provider id.
func (p *Pool) Get(ctx context.Context, id string) (model.BaseChatModel, error) {
	prov, err := p.Resolve(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.cache[prov.ID]; ok {
		return m, nil
	}
	m, err := p.build(ctx, prov)
	if err != nil {
		return nil, err
	}
	p.cache[prov.ID] = m
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
// Every agent in a run shares the provider's client and gets its own
// telemetry-recording wrapper so a trace can attribute each call to an agent.
//
// The mock provider is the exception: it scripts each role differently, so it
// builds a fresh model per agent.
func (p *Pool) ModelBuilder(ctx context.Context, id string, rec Recorder) (func(role, agentID string) model.BaseChatModel, error) {
	prov, err := p.Resolve(id)
	if err != nil {
		return nil, err
	}
	if p.mock {
		return func(role, agentID string) model.BaseChatModel {
			return wrap(newMockModel(role), agentID, prov, rec)
		}, nil
	}
	if !prov.Ready() {
		return nil, fmt.Errorf("provider: %q has no base_url or model configured; open Settings and finish setting it up", prov.ID)
	}

	shared, err := p.Get(ctx, prov.ID)
	if err != nil {
		return nil, err
	}
	return func(role, agentID string) model.BaseChatModel {
		return wrap(shared, agentID, prov, rec)
	}, nil
}

func buildOpenAI(ctx context.Context, p config.Provider) (model.BaseChatModel, error) {
	if !p.Ready() {
		return nil, fmt.Errorf("provider: %q needs a base_url and a model", p.ID)
	}
	m, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL: strings.TrimSpace(p.BaseURL),
		APIKey:  strings.TrimSpace(p.APIKey),
		Model:   strings.TrimSpace(p.Model),
		Timeout: p.Timeout(),
	})
	if err != nil {
		return nil, fmt.Errorf("provider: build %q: %w", p.ID, err)
	}
	return m, nil
}

// ---------- telemetry ----------

// wrap returns m instrumented to report every call to rec. A nil recorder
// returns m untouched so there is no cost when nobody is watching.
func wrap(m model.BaseChatModel, agentID string, p config.Provider, rec Recorder) model.BaseChatModel {
	if rec == nil {
		return m
	}
	return &recordingModel{inner: m, agentID: agentID, providerID: p.ID, model: p.Model, rec: rec}
}

// recordingModel times each request and reports its shape. It deliberately
// records sizes rather than prompts: enough to explain a slow or failed turn
// without copying whole conversations into the database.
type recordingModel struct {
	inner      model.BaseChatModel
	agentID    string
	providerID string
	model      string
	rec        Recorder
}

func (r *recordingModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	start := time.Now()
	out, err := r.inner.Generate(ctx, in, opts...)
	rec := r.base(in, start)
	rec.Err = err
	if out != nil {
		rec.OutputChars = len(out.Content) + len(out.ReasoningContent)
	}
	r.rec(rec)
	return out, err
}

func (r *recordingModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	start := time.Now()
	stream, err := r.inner.Stream(ctx, in, opts...)
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
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				rec := r.base(in, start)
				rec.OutputChars = chars
				if !isStreamEnd(recvErr) {
					rec.Err = recvErr
					sw.Send(nil, recvErr)
				}
				r.rec(rec)
				return
			}
			if msg != nil {
				chars += len(msg.Content) + len(msg.ReasoningContent)
			}
			if closed := sw.Send(msg, nil); closed {
				rec := r.base(in, start)
				rec.OutputChars = chars
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

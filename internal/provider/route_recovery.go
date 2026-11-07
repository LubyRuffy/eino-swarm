package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// RouteRecoveryExhaustedError ends one logical model call after the confirmed
// route-refusal budget. The turn must stop; this is not another model retry.
type RouteRecoveryExhaustedError struct {
	Attempts int
	Err      error
}

func (e *RouteRecoveryExhaustedError) Error() string {
	if e == nil || e.Err == nil {
		return "no healthy provider can serve this model"
	}
	return fmt.Sprintf("no healthy provider after %d consecutive route refusals: %v", e.Attempts, e.Err)
}

func (e *RouteRecoveryExhaustedError) Unwrap() error { return e.Err }

// A routing refusal is observed at the HTTP boundary, not inferred from error
// prose or a duration that includes origin cooldown. Keep only this fixed,
// bounded error envelope, never a successful model response.
type routeRejectionKey struct{}
type routeRejection struct{ confirmed atomic.Bool }
type routeRecoveryDisabledKey struct{}

// WithoutRouteRecovery keeps optional auxiliary calls on their existing
// request policy. They still wait for and update the shared origin gate, but a
// routing refusal cannot start a recovery loop without an active turn to Stop.
func WithoutRouteRecovery(ctx context.Context) context.Context {
	return context.WithValue(ctx, routeRecoveryDisabledKey{}, true)
}

type routeUnavailableError struct{ error }

func (e *routeUnavailableError) Unwrap() error { return e.error }

func observeRouteRejection(ctx context.Context) (context.Context, *routeRejection) {
	observation := &routeRejection{}
	return context.WithValue(ctx, routeRejectionKey{}, observation), observation
}

func (r *routeRejection) wrap(err error) error {
	if err != nil && r.confirmed.Load() {
		return &routeUnavailableError{err}
	}
	return err
}

func isRouteUnavailable(err error) bool {
	var refusal *routeUnavailableError
	return errors.As(err, &refusal)
}

type routeRejectionBody struct {
	io.ReadCloser
	observation *routeRejection
	deadline    time.Time
	raw         []byte
	oversized   bool
}

func (b *routeRejectionBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	const maxRejectionBytes = 4096
	if len(b.raw)+n <= maxRejectionBytes && !b.oversized {
		b.raw = append(b.raw, p[:n]...)
	} else {
		b.oversized, b.raw = true, nil
	}
	if err == io.EOF && !b.oversized && time.Now().Before(b.deadline) {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b.raw, &envelope) == nil &&
			envelope.Error.Code == "provider_unavailable" &&
			envelope.Error.Message == "no healthy provider can serve this model" {
			b.observation.confirmed.Store(true)
		}
	}
	return n, err
}

func observeRouteResponse(req *http.Request, resp *http.Response, started time.Time, firstByte time.Duration) {
	observation, _ := req.Context().Value(routeRejectionKey{}).(*routeRejection)
	if observation != nil && firstByte > 0 && resp.StatusCode == http.StatusServiceUnavailable && resp.Body != nil {
		resp.Body = &routeRejectionBody{ReadCloser: resp.Body, observation: observation, deadline: started.Add(firstByte)}
	}
}

// Stop, the caller's context, a different error, or the consecutive refusal
// budget ends this wait. Request header/idle budgets remain per send.
// A probe that returns any other failure goes back to normal bounded retries.
func (r *recordingModel) retryRoute(ctx context.Context, err error) bool {
	return r.gate != nil && ctx.Value(isolatedRecoveryKey{}) != true && ctx.Value(routeRecoveryDisabledKey{}) != true && ctx.Err() == nil && isRouteUnavailable(err)
}

func (r *recordingModel) generate(ctx context.Context, in []*schema.Message, opts []model.Option) (*schema.Message, error) {
	refusals := 0
	for {
		out, err := r.generateOnce(ctx, in, opts)
		if !r.retryRoute(ctx, err) {
			return out, err
		}
		refusals++
		if refusals >= config.DefaultRouteRecoveryAttempts {
			return out, &RouteRecoveryExhaustedError{Attempts: refusals, Err: err}
		}
	}
}

func (r *recordingModel) stream(ctx context.Context, in []*schema.Message, opts []model.Option) (*schema.StreamReader[*schema.Message], error) {
	refusals := 0
	for {
		out, err := r.streamOnce(ctx, in, opts)
		if !r.retryRoute(ctx, err) {
			return out, err
		}
		refusals++
		if refusals >= config.DefaultRouteRecoveryAttempts {
			return nil, &RouteRecoveryExhaustedError{Attempts: refusals, Err: err}
		}
	}
}

// RouteUnavailable reports an observed routing outage on this provider's
// origin. Automatic post-turn work can skip it without spending another probe.
// It never creates a gate or sends a request; unknown providers are not evidence
// of a routing outage. A complete successful probe clears the observation.
func (p *Pool) RouteUnavailable(providerID string) bool {
	prov, err := p.Resolve(providerID)
	if err != nil {
		return false
	}
	p.mu.Lock()
	gate := p.backoffs[backoffKey(prov.BaseURL)]
	p.mu.Unlock()
	if gate == nil {
		return false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.routeUnavailable
}

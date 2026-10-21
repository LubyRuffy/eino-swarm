package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

// BackoffRecord describes a wait before a request, not a model call. The
// recorder binds it to the actual agent and turn without storing request data.
type BackoffRecord struct {
	Attempt int   `json:"attempt"`
	WaitMS  int64 `json:"wait_ms"`
}

type backoffObserverKey struct{}
type reconnectBackoffKey struct{}

func backoffKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	// Models and provider aliases on one server compete for the same capacity.
	scheme, host, port := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port()
	if port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

// Called under Pool.mu. Settings may replace clients, but do not cure a server.
func (p *Pool) backoffFor(baseURL string) *backoffGate {
	if p.backoffs == nil {
		p.backoffs = make(map[string]*backoffGate)
	}
	key := backoffKey(baseURL)
	if p.backoffs[key] == nil {
		p.backoffs[key] = newBackoffGate(config.DefaultModelBackoffInitial, config.DefaultModelBackoffMax)
	}
	return p.backoffs[key]
}

func (r *recordingModel) admit(ctx context.Context) (context.Context, func(error), error) {
	if r.rec != nil {
		ctx = context.WithValue(ctx, backoffObserverKey{}, func(attempt int, delay time.Duration) {
			r.rec(CallRecord{AgentID: r.agentID, ProviderID: r.providerID, Model: r.model,
				Backoff: &BackoffRecord{Attempt: attempt, WaitMS: delay.Milliseconds()}})
		})
	}
	if r.gate == nil {
		return ctx, func(error) {}, nil
	}
	onWait, _ := ctx.Value(backoffObserverKey{}).(func(int, time.Duration))
	ticket, err := r.gate.acquire(ctx, onWait)
	if err != nil {
		return ctx, nil, err
	}
	// The HTTP adapter's one fresh-connection retry belongs to this same
	// attempt. Release its failed permit before waiting for another probe.
	ctx = context.WithValue(ctx, reconnectBackoffKey{}, func() error {
		ticket.finish(backoffFailure)
		var err error
		ticket, err = r.gate.acquire(ctx, onWait)
		return err
	})
	return ctx, func(err error) {
		switch {
		case ctx.Err() != nil, errors.Is(err, context.Canceled):
			ticket.finish(backoffCanceled)
		case isBackoffFailure(err):
			ticket.finish(backoffFailure)
		case err == nil:
			ticket.finish(backoffSuccess)
		default:
			ticket.finish(backoffCanceled)
		}
	}, nil
}

// This sees only errors inside a model call. Tool decoder errors and context
// overflow happen outside it or are explicit 4xx API rejections.
func isBackoffFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var transport net.Error
	// A response-header timeout can match DeadlineExceeded even while the
	// caller is live. Only that caller's cancellation proves a human stop.
	if errors.As(err, &transport) {
		cause := error(transport)
		for errors.Unwrap(cause) != nil {
			cause = errors.Unwrap(cause)
		}
		if cause != context.DeadlineExceeded {
			return true
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var api *einoopenai.APIError
	var sdk *openai.APIError
	var request *openai.RequestError
	status := 0
	switch {
	case errors.As(err, &api):
		status = api.HTTPStatusCode
	case errors.As(err, &sdk):
		status = sdk.HTTPStatusCode
	case errors.As(err, &request):
		status = request.HTTPStatusCode
	}
	if status >= 300 {
		return status == 408 || status == 429 || status >= 500
	}
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.Is(err, ErrIdleTimeout) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &syntax) || errors.As(err, &shape) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unexpected eof") || strings.Contains(s, "unexpected end of json") ||
		strings.Contains(s, "invalid character") || strings.Contains(s, "mismatch type ") ||
		strings.Contains(s, "json: cannot unmarshal")
}

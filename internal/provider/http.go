package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// ErrIdleTimeout is a model call that produced headers (or earlier
// chunks) and then went silent. Distinct from a total http.Client.Timeout,
// which would also kill a thinking stream that is still emitting tokens.
var ErrIdleTimeout = errors.New("provider: stream idle timeout")

// IdleTimeoutError is how long the model stayed silent. errors.Is(err,
// ErrIdleTimeout) holds through eino's "failed to receive stream chunk" wrap.
type IdleTimeoutError struct {
	Idle time.Duration
}

func (e IdleTimeoutError) Error() string {
	return fmt.Sprintf("%v after %s", ErrIdleTimeout, e.Idle)
}

func (e IdleTimeoutError) Unwrap() error { return ErrIdleTimeout }

// chatHTTPClient bounds silence, not the whole streamed body.
//
// eino's ChatModelConfig.Timeout becomes http.Client.Timeout, and that clock
// includes reading the body. A reasoning model that thinks for longer than
// the setting dies mid-thought with "failed to receive stream chunk: context
// deadline exceeded (Client.Timeout …)", even while tokens are still arriving.
// The first byte gets a short budget. After headers arrive, silence
// between chunks gets `idle`. A live stream can outlast the first-byte cap.
func chatHTTPClient(idle time.Duration) *http.Client {
	if idle <= 0 {
		idle = config.DefaultRequestTimeout
	}
	return &http.Client{
		Transport: &idleTransport{base: chatTransport(idle), idle: idle},
	}
}

func chatTransport(idle time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: firstByteTimeout(idle),
	}
}

// firstByteTimeout is the wait for response headers. It never outgrows the
// stream idle budget, and it never inherits a multi-minute idle setting:
// this endpoint sends headers in well under a second when the connection
// is alive.
func firstByteTimeout(idle time.Duration) time.Duration {
	cap := config.DefaultFirstByteTimeout
	if idle > 0 && idle < cap {
		return idle
	}
	return cap
}

type idleTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *idleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A dead HTTP/2 connection sits until ResponseHeaderTimeout and then
	// fails with "timeout awaiting response headers". The request never
	// reached a handler, so one fresh connection is the recovery.
	req, err := replayable(req)
	if err != nil {
		return nil, err
	}
	resp, err := t.roundTrip(req)
	if err == nil || !headerWaitTimedOut(err) || req.GetBody == nil {
		return resp, err
	}
	next := req.Clone(req.Context())
	next.Close = true
	body, berr := req.GetBody()
	if berr != nil {
		return nil, err
	}
	next.Body = body
	next.GetBody = req.GetBody
	return t.roundTrip(next)
}

func (t *idleTransport) roundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body = newIdleBody(resp.Body, t.idle)
	}
	return resp, nil
}

func replayable(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.GetBody != nil {
		return req, nil
	}
	raw, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	req.Body, err = req.GetBody()
	return req, err
}

func headerWaitTimedOut(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout awaiting response headers")
}

type idleBody struct {
	inner io.ReadCloser
	idle  time.Duration

	mu       sync.Mutex
	timer    *time.Timer
	timedOut bool
	closed   bool
}

func newIdleBody(inner io.ReadCloser, idle time.Duration) *idleBody {
	b := &idleBody{inner: inner, idle: idle}
	b.timer = time.AfterFunc(idle, b.onIdle)
	return b
}

func (b *idleBody) onIdle() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.timedOut = true
	b.mu.Unlock()
	// Closing unblocks a Read waiting for the next chunk.
	_ = b.inner.Close()
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	b.mu.Lock()
	timedOut := b.timedOut
	if n > 0 && !timedOut && !b.closed {
		b.timer.Reset(b.idle)
	}
	b.mu.Unlock()
	if timedOut && err != nil {
		return n, IdleTimeoutError{Idle: b.idle}
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.mu.Lock()
	b.closed = true
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	return b.inner.Close()
}

package provider

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
// Headers and the next byte each get `idle`; a live stream can outlast it.
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
		ResponseHeaderTimeout: idle,
	}
}

type idleTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *idleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body = newIdleBody(resp.Body, t.idle)
	}
	return resp, nil
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

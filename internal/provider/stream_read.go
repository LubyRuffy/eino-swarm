package provider

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// StreamReadTimeoutError is a transport timeout while reading a successful
// streaming HTTP response. Header, caller and configured idle deadlines are separate.
type StreamReadTimeoutError struct{ Cause error }

func (e *StreamReadTimeoutError) Error() string { return "provider: model response read timed out" }
func (e *StreamReadTimeoutError) Unwrap() error { return e.Cause }

type streamReadTrace struct {
	// Set before the successful HTTP response body becomes readable.
	requestID string

	mu            sync.Mutex
	receivedBytes int64
	lastReceived  time.Time
	readErr       error
}

func streamRequestID(headers http.Header) string {
	values := headers.Values("X-Request-ID")
	// AIGateway issues 128-bit hexadecimal IDs. Ignore arbitrary header text.
	if len(values) != 1 || len(values[0]) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(values[0]); err != nil {
		return ""
	}
	return values[0]
}

func (s *streamReadTrace) observeRead(ctx context.Context, n int, err error) error {
	if err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		var network net.Error
		if errors.As(err, &network) && network.Timeout() {
			err = &StreamReadTimeoutError{Cause: err}
		}
	}
	s.mu.Lock()
	if n > 0 {
		s.receivedBytes += int64(n)
		s.lastReceived = time.Now()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		s.readErr = err
	}
	s.mu.Unlock()
	return err
}

// traceError adds HTTP evidence only to the call record. The error sent
// to the consumer keeps its original classification and contains no receipt.
func (s *streamCompletion) traceError(ctx context.Context, err error) error {
	if !s.http.Load() {
		return err
	}
	var incomplete *ResponsesIncompleteError
	terminal := errors.As(err, &incomplete)
	if !terminal && (ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var label string
	var timeout *StreamReadTimeoutError
	var idle IdleTimeoutError
	switch {
	case terminal:
		label = incomplete.Error()
	case errors.As(err, &timeout):
		label = timeout.Error()
	case errors.As(err, &idle):
		label = idle.Error()
	case errors.Is(err, io.ErrUnexpectedEOF):
		label = io.ErrUnexpectedEOF.Error()
	case s.readErr != nil && errors.Is(err, s.readErr):
		label = "provider: model response body read failed"
	default:
		return err
	}
	if s.requestID != "" {
		label += " request_id=" + s.requestID
	}
	label += fmt.Sprintf(" received_bytes=%d", s.receivedBytes)
	if !s.lastReceived.IsZero() {
		label += " last_received_at=" + s.lastReceived.UTC().Format(time.RFC3339Nano)
	}
	return &streamTraceError{label: label, cause: err}
}

type streamTraceError struct {
	label string
	cause error
}

func (e *streamTraceError) Error() string { return e.label }
func (e *streamTraceError) Unwrap() error { return e.cause }

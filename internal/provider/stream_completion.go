package provider

import (
	"io"
	"sync/atomic"
)

type streamCompletionKey struct{}

type streamCompletion struct {
	http atomic.Bool
	done atomic.Bool
}

// The SDK maps both a protocol terminator and a truncated HTTP body to EOF.
// Observe only the fixed SSE terminator; never retain response content.
type streamCompletionBody struct {
	io.ReadCloser
	completion *streamCompletion
	offset     int
	invalid    bool
}

func (b *streamCompletionBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	const marker = "data:[DONE]"
	for _, ch := range p[:n] {
		if ch == '\n' {
			if !b.invalid && b.offset == len(marker) {
				b.completion.done.Store(true)
			}
			b.offset, b.invalid = 0, false
			continue
		}
		if b.invalid {
			continue
		}
		if (ch == ' ' || ch == '\t' || ch == '\r') && (b.offset == 0 || b.offset == len("data:") || b.offset == len(marker)) {
			continue
		}
		if b.offset < len(marker) && ch == marker[b.offset] {
			b.offset++
		} else {
			b.invalid = true
		}
	}
	return n, err
}

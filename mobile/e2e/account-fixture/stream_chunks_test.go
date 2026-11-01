package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeAccountStreamFlushesShortAndSplitUTF8ChunksBeforeCompletion(t *testing.T) {
	f, server, token := webFixture(t)
	webRequest(t, server, "/fixture/control?model=chunked", "", "", http.StatusOK)
	for call := 1; call <= 2; call++ {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"native-fixture-model","stream":true,"messages":[{"role":"user","content":"native stream probe"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(res.Body)
		first, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(first, "Native streamed ") {
			res.Body.Close()
			t.Fatalf("first flushed model frame: %q %v", first, err)
		}
		f.mu.Lock()
		completedEarly := f.counts["model_chunked_completed"] >= call
		f.mu.Unlock()
		if completedEarly {
			res.Body.Close()
			t.Fatal("native model fixture buffered the entire completion before exposing its first frame")
		}
		rest, err := io.ReadAll(reader)
		res.Body.Close()
		raw := first + string(rest)
		if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(raw, `"content":"中文🙂 complete"`) || !strings.HasSuffix(raw, "data: [DONE]\n\n") {
			t.Fatalf("native chunked model lost UTF8 or terminal frame: %q %v", raw, err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, expected := range map[string]int{"gateway_model_calls": 2, "model_chunk_flushes": 24, "model_chunked_completed": 2, "model_successes": 2, "model_chunk_cancelled": 0, "model_chunk_failed": 0} {
		if f.counts[key] != expected {
			t.Errorf("%s=%d want %d", key, f.counts[key], expected)
		}
	}
}

// Every Flush records its exact body boundary. A text-only or whole-response
// fixture cannot regress Foundation Data's advanced startIndex between callbacks.
type streamChunkWriter struct {
	header               http.Header
	chunks               [][]byte
	buffer               bytes.Buffer
	failWrite, failFlush bool
}

func (w *streamChunkWriter) Header() http.Header { return w.header }
func (w *streamChunkWriter) WriteHeader(int)     {}
func (w *streamChunkWriter) Write(raw []byte) (int, error) {
	if w.failWrite {
		return 0, io.ErrClosedPipe
	}
	return w.buffer.Write(raw)
}
func (w *streamChunkWriter) FlushError() error {
	if w.failFlush {
		return io.ErrClosedPipe
	}
	w.chunks = append(w.chunks, bytes.Clone(w.buffer.Bytes()))
	w.buffer.Reset()
	return nil
}

func TestNativeChunkedStreamKeepsExactUTF8BoundariesAndStopsFailedWriters(t *testing.T) {
	for _, failure := range []string{"write", "flush", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			f := &fixture{counts: map[string]int{}}
			w := &streamChunkWriter{header: make(http.Header), failWrite: failure == "write", failFlush: failure == "flush"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "cancel" {
				cancel()
			}
			f.mu.Lock()
			f.chunkedModel(w, (&http.Request{}).WithContext(ctx))
			f.mu.Unlock()
			key := "model_chunk_failed"
			if failure == "cancel" {
				key = "model_chunk_cancelled"
			}
			if f.counts[key] != 1 || f.counts["model_successes"] != 0 {
				t.Fatalf("failed native stream was accepted: %v", f.counts)
			}
		})
	}

	f := &fixture{counts: map[string]int{}}
	w := &streamChunkWriter{header: make(http.Header)}
	started := time.Now()
	f.mu.Lock()
	f.chunkedModel(w, (&http.Request{}).WithContext(t.Context()))
	f.mu.Unlock()
	if len(w.chunks) != 12 || !bytes.Equal(w.chunks[1], []byte("d")) {
		t.Fatalf("native stream lost the short second chunk: %q", w.chunks)
	}
	if !bytes.Equal(w.chunks[3], []byte{0xe4}) || !bytes.Equal(w.chunks[4], []byte{0xb8, 0xad}) || !bytes.Equal(w.chunks[7], []byte{0xf0}) || !bytes.Equal(w.chunks[8], []byte{0x9f, 0x99}) || !bytes.Equal(w.chunks[9], []byte{0x82}) {
		t.Fatalf("native stream no longer crosses UTF8 callback boundaries: %q", w.chunks)
	}
	if time.Since(started) < time.Second || !bytes.Contains(bytes.Join(w.chunks, nil), []byte("中文🙂 complete")) || f.counts["model_chunked_completed"] != 1 {
		t.Fatalf("native stream lost delayed flushes or complete UTF8: %v", f.counts)
	}
}

package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Responses must honor the same provider wait as chat completions, even when
// preparing a response takes longer than the later stream's idle allowance.
func TestResponsesAdapterAllowsFirstByteWaitLongerThanIdle(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" {
					t.Errorf("request path=%q", r.URL.Path)
				}
				time.Sleep(1100 * time.Millisecond)
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", responsesTimeoutFixture)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, responsesTimeoutFixture)
				}
			}))
			t.Cleanup(srv.Close)
			m, err := buildOpenAI(context.Background(), config.Provider{
				ID: "fixture", BaseURL: srv.URL + "/v1", Model: "fixture", APIMode: config.APIModeResponses,
				TimeoutSeconds: 1, FirstByteTimeoutSeconds: 3,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			out, err := runResponsesTimeoutCall(ctx, m, streaming)
			if err != nil || out != "ready" {
				t.Fatalf("response starting after idle allowance: content=%q err=%v", out, err)
			}
		})
	}
}

// A separately configured shorter wait must reach the actual HTTP transport;
// a caller deadline must not hide an adapter that silently kept the default.
func TestResponsesAdapterRetainsConfiguredFirstByteTimeout(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)
			m, err := buildOpenAI(context.Background(), config.Provider{
				ID: "fixture", BaseURL: srv.URL, Model: "fixture", APIMode: config.APIModeResponses,
				TimeoutSeconds: 3, FirstByteTimeoutSeconds: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_, err = runResponsesTimeoutCall(ctx, m, streaming)
			var first FirstByteTimeoutError
			if !errors.As(err, &first) || first.Wait != time.Second || !headerWaitTimedOut(err) {
				t.Fatalf("Responses adapter lost configured first-byte timeout: %v", err)
			}
			if attempts.Load() != 2 {
				t.Fatalf("requests=%d; header reconnect must remain bounded", attempts.Load())
			}
		})
	}
}

func runResponsesTimeoutCall(ctx context.Context, m model.BaseChatModel, streaming bool) (string, error) {
	in := []*schema.Message{schema.UserMessage("continue")}
	if !streaming {
		out, err := m.Generate(ctx, in)
		if err != nil {
			return "", err
		}
		return out.Content, nil
	}
	stream, err := m.Stream(ctx, in)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var content string
	for {
		out, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return content, nil
		}
		if err != nil {
			return content, err
		}
		content += out.Content
	}
}

const responsesTimeoutFixture = `{"id":"resp","status":"completed","output":[{"id":"msg","type":"message","role":"assistant","content":[{"type":"output_text","text":"ready"}]}]}`

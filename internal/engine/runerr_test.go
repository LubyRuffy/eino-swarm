package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestPublicTurnErrorRewritesASilentStream(t *testing.T) {
	inner := provider.IdleTimeoutError{Idle: 5 * time.Minute}
	wrapped := fmt.Errorf("[NodeRunError] failed to receive stream chunk: %w\nnode path: [node_1, ChatModel]", inner)
	got := publicTurnError(wrapped)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node_1", "node path"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the graph dump leaked: %s", got)
		}
	}
	if !strings.Contains(got, "Settings") || !strings.Contains(got, "thinking") {
		t.Fatalf("must say how to continue: %s", got)
	}
}

func TestPublicTurnErrorRewritesAHeaderTimeout(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] failed to create chat completion: Post \"https://example.invalid/v1/chat/completions\": http2: timeout awaiting response headers\n---------------- node path: [node_a, ChatModel]")
	got := publicTurnError(err)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node_a", "example.invalid", "http2"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the transport dump leaked: %s", got)
		}
	}
	if !strings.Contains(got, "first byte") || !strings.Contains(got, config.DefaultFirstByteTimeout.String()) {
		t.Fatalf("must name the first-byte budget: %s", got)
	}
}

func TestPublicTurnErrorNamesAContextWindowRejection(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: This model's maximum context length is 8192 tokens. However, you requested 9000 tokens\nnode path: [node_1, ChatModel]")
	got := publicTurnError(err)
	if got != publicContextOverflow {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"NodeRunError", "ChatModel", "8192", "9000", "node path"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the provider paragraph leaked into the typed error: %s", got)
		}
	}
}

func TestPublicTurnErrorLeavesAnUnrelatedFailure(t *testing.T) {
	err := fmt.Errorf("the endpoint refused the connection")
	if got := publicTurnError(err); got != err.Error() {
		t.Fatalf("got %q", got)
	}
	if publicTurnError(nil) != "" {
		t.Fatal("nil must stay blank")
	}
}

func TestPublicTurnErrorKeepsProviderJSONDetailsWithoutGuessingToolCause(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 54 (char 53)\nnode path: [node_1, ChatModel]")
	got := publicTurnError(err)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node_1", "node path", "a tool call"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the graph dump leaked: %s", got)
		}
	}
	if !strings.Contains(got, "400") || !strings.Contains(got, "column 54") {
		t.Fatalf("must preserve the actual provider rejection: %s", got)
	}
}

func TestPublicTurnErrorNamesTheModelService(t *testing.T) {
	cases := []struct {
		msg  string
		want string
		leak string
	}{
		{"[NodeRunError] error, status code: 429, status: 429 Too Many Requests, message: rate limited\nnode path: [node_1, ChatModel]", publicRateLimited, "429"},
		{"[NodeRunError] error, status code: 503, status: 503 Service Unavailable, message: busy\nnode path: [node_1, ChatModel]", publicModelUnavailable, "503"},
		{"[NodeRunError] failed to receive stream chunk: read tcp 192.0.2.1:1->192.0.2.2:443: read: operation timed out\nnode path: [node_1, ChatModel]", publicModelTransport, "192.0.2.1"},
	}
	for _, tc := range cases {
		got := publicTurnError(errors.New(tc.msg))
		if got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
		for _, leak := range []string{"NodeRunError", "ChatModel", "node path", tc.leak} {
			if strings.Contains(got, leak) {
				t.Fatalf("the provider dump leaked into %q: %s", got, leak)
			}
		}
	}
}

func TestPublicTurnErrorRewritesTheIterationCap(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] run node[ChatModel] pre processor fail: exceeds max iterations\n------------------------\nnode path: [ChatModel]")
	got := publicTurnError(err)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node path", "----", "pre processor"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the graph dump leaked: %s", got)
		}
	}
	if !strings.Contains(got, "tool-round cap") {
		t.Fatalf("got %q", got)
	}
}

func TestPublicTurnErrorExplainsContextOverflow(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] error, status code: 400: maximum context length is 262144 tokens; requested 64000 output tokens and prompt contains 198145 input tokens\nnode path: [node_1, ChatModel]")
	got := publicTurnError(err)
	if got != publicContextOverflow {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"NodeRunError", "node path", "198145", "status code: 400"} {
		if strings.Contains(got, leak) {
			t.Fatalf("raw provider error leaked: %q", got)
		}
	}
}

func TestPublicTurnErrorStripsAGraphDump(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] the endpoint refused the connection\nnode path: [node_1, ChatModel]")
	got := publicTurnError(err)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node path"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the graph dump leaked: %s", got)
		}
	}
	if !strings.Contains(got, "the endpoint refused the connection") {
		t.Fatalf("the public reason vanished: %s", got)
	}
}

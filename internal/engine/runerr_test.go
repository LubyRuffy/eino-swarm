package engine

import (
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

func TestPublicTurnErrorLeavesAnUnrelatedFailure(t *testing.T) {
	err := fmt.Errorf("the endpoint refused the connection")
	if got := publicTurnError(err); got != err.Error() {
		t.Fatalf("got %q", got)
	}
	if publicTurnError(nil) != "" {
		t.Fatal("nil must stay blank")
	}
}

func TestPublicTurnErrorRewritesInvalidToolJSON(t *testing.T) {
	err := fmt.Errorf("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 54 (char 53)\nnode path: [node_1, ChatModel]")
	got := publicTurnError(err)
	for _, leak := range []string{"NodeRunError", "ChatModel", "node_1", "node path", "column 54"} {
		if strings.Contains(got, leak) {
			t.Fatalf("the graph dump leaked: %s", got)
		}
	}
	if !strings.Contains(strings.ToLower(got), "json") {
		t.Fatalf("must say the tool call was invalid JSON: %s", got)
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

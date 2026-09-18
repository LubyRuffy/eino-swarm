package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

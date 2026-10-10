package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

func TestPublicTurnErrorPreservesHTTPFailureInsteadOfGuessingToolJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"empty gateway response", &openai.RequestError{HTTPStatusCode: 502, HTTPStatus: "502 Bad Gateway", Err: errors.New("unexpected end of JSON input")}, "HTTP 502 Bad Gateway: unexpected end of JSON input"},
		{"invalid gateway body", &openai.RequestError{HTTPStatusCode: 503, Err: errors.New("invalid character '<' looking for beginning of value"), Body: []byte("<html>private-response-secret</html>")}, "HTTP 503 Service Unavailable: invalid character '<'"},
		{"API rejection", &einoopenai.APIError{HTTPStatusCode: 400, Message: "Unterminated string at line 1 column 54"}, "HTTP 400 Bad Request: Unterminated string at line 1 column 54"},
		{"SDK authentication", &openai.APIError{HTTPStatusCode: 401, Message: "Invalid API key"}, "HTTP 401 Unauthorized: Invalid API key"},
		{"permission", &einoopenai.APIError{HTTPStatusCode: 403, Message: "model access denied"}, "HTTP 403 Forbidden: model access denied"},
		{"quota", &einoopenai.APIError{HTTPStatusCode: 429, Message: "request quota exceeded"}, "HTTP 429 Too Many Requests: request quota exceeded"},
		{"missing decode cause", &openai.RequestError{HTTPStatusCode: 500, Body: []byte("private-response-secret")}, "HTTP 500 Internal Server Error"},
		{"unknown status", &openai.RequestError{HTTPStatusCode: 529, Err: errors.New("upstream overloaded")}, "HTTP 529: upstream overloaded"},
		{"API message only", &einoopenai.APIError{Message: "upstream refused request"}, "model API request failed: upstream refused request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("[NodeRunError] failed to receive: %w\nnode path: [node_private, ChatModel]", tc.err)
			got := publicTurnError(wrapped)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("concrete API cause lost: %q", got)
			}
			for _, forbidden := range []string{"a tool call", "private-response-secret", "node_private", "node path", "NodeRunError", "ChatModel", "<nil>"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("unproven cause or private dump leaked: %q", got)
				}
			}
		})
	}
}

func TestPublicTurnErrorDoesNotInferAToolCauseFromLocalJSONFailure(t *testing.T) {
	got := publicTurnError(errors.New("unexpected end of JSON input"))
	if got != "unexpected end of JSON input" {
		t.Fatalf("JSON alone does not identify a tool call: %q", got)
	}
}

func TestPublicTurnErrorRemovesNestedGraphWrappersWithoutGuessingTheCause(t *testing.T) {
	const cause = "error, status code: 400, message: Unterminated string at column 66"
	for _, prefix := range []string{
		"[NodeRunError] [NodeRunError] ",
		"[GraphRunError] [NodeRunError] [GraphRunError] ",
		" [NodeRunError]  [GraphRunError] ",
	} {
		got := publicTurnError(errors.New(prefix + cause + "\nnode path: [private, ChatModel]"))
		if got != cause {
			t.Fatalf("nested graph wrapper changed or leaked into the public cause: %q", got)
		}
	}
}

func TestPublicAPICauseIsBoundedAndDoesNotExposeCredentials(t *testing.T) {
	message := "Authorization: Bearer secret-auth API_KEY=secret-key Incorrect API key provided: secret-echo password=secret-password cookie=session=secret-cookie sk-secret-provider " + strings.Repeat("detail ", 200)
	got := publicTurnError(&einoopenai.APIError{HTTPStatusCode: 401, Message: message})
	for _, secret := range []string{"secret-auth", "secret-key", "secret-echo", "secret-password", "secret-cookie", "sk-secret-provider"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential exposed in public API cause: %q", got)
		}
	}
	if !strings.Contains(got, "HTTP 401") || !strings.Contains(got, "detail") || len([]rune(got)) > 600 {
		t.Fatalf("must keep useful bounded cause: %q", got)
	}
}

func TestPublicAPICauseLimitIncludesTruncationMarker(t *testing.T) {
	const prefix = "model API request failed: "
	for _, size := range []int{511, 512, 513} {
		got := strings.TrimPrefix(formatModelAPIError(0, strings.Repeat("界", size)), prefix)
		if len([]rune(got)) > 512 {
			t.Fatalf("%d-rune cause exceeds its limit after adding a marker: %d", size, len([]rune(got)))
		}
		if size <= 512 && got != strings.Repeat("界", size) {
			t.Fatalf("a cause within the limit changed: %q", got)
		}
		if size > 512 && got != strings.Repeat("界", 511)+"…" {
			t.Fatalf("truncated cause did not retain its marker within the limit: %q", got)
		}
	}
}

func TestPublicAPIHeaderTimeoutKeepsFirstByteAdvice(t *testing.T) {
	got := publicTurnError(&openai.RequestError{Err: errors.New("http2: timeout awaiting response headers")})
	if !strings.Contains(got, "first byte") || strings.Contains(got, "http2") {
		t.Fatalf("header timeout recovery regressed: %q", got)
	}
}

func TestPublicAPIContextOverflowKeepsRecoveryAdvice(t *testing.T) {
	got := publicTurnError(&einoopenai.APIError{HTTPStatusCode: 400, Message: "maximum context length is 128000 tokens"})
	if !strings.Contains(got, "automatic compact") || !strings.Contains(got, "Settings") {
		t.Fatalf("context recovery regressed: %q", got)
	}
}

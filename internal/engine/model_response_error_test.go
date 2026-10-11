package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

func TestModelResponseClassificationKeepsSDKAndToolDecoderProvenance(t *testing.T) {
	decoder := errors.New("unexpected end of JSON input")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"request response decode", &openai.RequestError{HTTPStatusCode: 400, Err: decoder}, modelRetryReasonResponse},
		{"stream response decode", &provider.CallError{Cause: decoder}, modelRetryReasonResponse},
		{"tool decoder", decoder, modelRetryReasonInvalidToolJSON},
		{"parsed API tool rejection", &provider.CallError{Cause: &einoopenai.APIError{HTTPStatusCode: 400, Message: "Unterminated string in tool arguments"}}, modelRetryReasonInvalidToolJSON},
		{"parsed SDK tool rejection", &provider.CallError{Cause: &openai.APIError{HTTPStatusCode: 400, Message: "Unterminated string in tool arguments"}}, modelRetryReasonInvalidToolJSON},
		{"unrelated model failure", &provider.CallError{Cause: errors.New("unknown endpoint failure")}, ""},
		{"service unavailable", &openai.RequestError{HTTPStatusCode: 502, Err: decoder}, modelRetryReasonUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("graph model: %w", tc.err)
			if got := modelFailureClass(err); got != tc.want {
				t.Fatalf("class=%q want=%q", got, tc.want)
			}
		})
	}
}

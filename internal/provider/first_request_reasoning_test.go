package provider

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
)

func TestFirstRequestReasoningDoesNotAddAnImplicitFallback(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "generate"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			srv, seen := effortRejectingServer(t)
			build, err := backoffPool(srv.URL).ModelBuilder(t.Context(), "fixture", "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			base := build("manager", "manager")
			derived := WithFirstRequestReasoningEffort(base, config.ReasoningLow)
			send := func(m model.BaseChatModel) error {
				if !streaming {
					_, err := m.Generate(t.Context(), backoffMessages())
					return err
				}
				stream, err := m.Stream(t.Context(), backoffMessages())
				if err != nil {
					return err
				}
				defer stream.Close()
				return finishBackoffStream(stream)
			}
			if err := send(derived); HTTPStatus(err) != http.StatusBadRequest {
				t.Fatalf("the explicit recovery option should surface its rejection: %v", err)
			}
			if got := seen(); !reflect.DeepEqual(got, []string{"low"}) {
				t.Fatalf("a rejected recovery request sent additional requests: %q", got)
			}
			if err := send(derived); err != nil {
				t.Fatalf("later request used the expired override: %v", err)
			}
			if err := send(base); err != nil {
				t.Fatalf("base model was changed: %v", err)
			}
			if got := seen(); !reflect.DeepEqual(got, []string{"low", "", ""}) {
				t.Fatalf("effort changed outside the first request: %q", got)
			}
		})
	}
}

func TestFirstRequestReasoningKeepsExplicitOrUnsupportedOptions(t *testing.T) {
	explicit := &recordingModel{reasoning: config.ReasoningHigh}
	plain := &recordingModel{}
	mock := newMockModel("manager")
	if WithFirstRequestReasoningEffort(explicit, config.ReasoningLow) != explicit ||
		WithFirstRequestReasoningEffort(plain, "") != plain ||
		WithFirstRequestReasoningEffort(plain, "unknown") != plain ||
		WithFirstRequestReasoningEffort(mock, config.ReasoningLow) != mock {
		t.Fatal("an explicit setting or model without a request adapter was replaced")
	}
}

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"reflect"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

func TestBackoffFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, false},
		{"canceled", fmt.Errorf("wrapped: %w", context.Canceled), false},
		{"deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), false},
		{"refused", &url.Error{Op: "Post", URL: "http://fixture", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}, true},
		{"transport cancellation", &url.Error{Op: "Post", Err: context.Canceled}, false},
		{"transport deadline", &url.Error{Op: "Post", Err: context.DeadlineExceeded}, false},
		{"provider header deadline", fmt.Errorf("model HTTP: %w", backoffProviderTimeout{}), true},
		{"idle", fmt.Errorf("stream: %w", IdleTimeoutError{Idle: time.Second}), true},
		{"EOF", io.EOF, true},
		{"truncated", fmt.Errorf("stream: %w", io.ErrUnexpectedEOF), true},
		{"syntax", &json.SyntaxError{Offset: 3}, true},
		{"shape", &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(0)}, true},
		{"EOF text", errors.New("stream: unexpected EOF"), true},
		{"empty JSON text", errors.New("unexpected end of JSON input"), true},
		{"HTML text", errors.New("invalid character '<' looking for beginning of value"), true},
		{"sonic field type", errors.New("Mismatch type int64 with value string"), true},
		{"JSON field type", errors.New("json: cannot unmarshal string into Go value of type int"), true},
		{"successful HTTP truncated response", &openai.RequestError{HTTPStatusCode: 200, Err: io.EOF}, true},
		{"successful HTTP invalid JSON", &openai.RequestError{HTTPStatusCode: 200, Err: &json.SyntaxError{Offset: 3}}, true},
		{"successful HTTP invalid field type", &openai.RequestError{HTTPStatusCode: 200, Err: &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(0)}}, true},
		{"other", errors.New("invalid tool configuration"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBackoffFailure(tc.err); got != tc.want {
				t.Fatalf("isBackoffFailure(%v)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
	for _, status := range []int{0, 400, 401, 403, 404, 408, 422, 429, 500, 503} {
		for name, err := range map[string]error{
			"eino":    &einoopenai.APIError{HTTPStatusCode: status, Message: "rejected"},
			"sdk":     &openai.APIError{HTTPStatusCode: status, Message: "rejected"},
			"request": &openai.RequestError{HTTPStatusCode: status, Err: errors.New("request failed")},
		} {
			t.Run(fmt.Sprintf("%s-%d", name, status), func(t *testing.T) {
				want := status == 408 || status == 429 || status >= 500
				if got := isBackoffFailure(fmt.Errorf("model call: %w", err)); got != want {
					t.Fatalf("status %d classified failure=%v, want %v", status, got, want)
				}
			})
		}
	}
	// A rejected client request stays terminal even if its body was not JSON.
	if isBackoffFailure(&openai.RequestError{HTTPStatusCode: 400, Err: errors.New("invalid character '<'")}) {
		t.Fatal("a 400 response must not enter a temporary outage loop")
	}
}

func TestBackoffKeySharesServerCapacityAndKeepsIndependentServersSeparate(t *testing.T) {
	for raw, want := range map[string]string{
		" HTTPS://Host.Example:8443/v1?key=private ": "https://host.example:8443",
		"https://Host.Example:8443/alternate":        "https://host.example:8443",
		"http://host.example:8443/v1":                "http://host.example:8443",
		"http://host.example:8444/v1":                "http://host.example:8444",
		"http://Host.Example:80/v1":                  "http://host.example",
		"http://Host.Example/v1":                     "http://host.example",
		"https://Host.Example:443/v1":                "https://host.example",
		"https://Host.Example/v1":                    "https://host.example",
		"http://Host.Example:443/v1":                 "http://host.example:443",
		"http://[::1]:80/v1":                         "http://[::1]",
		"http://[::1]/v1":                            "http://[::1]",
		"https://[2001:db8::1]:443/v1":               "https://[2001:db8::1]",
		"http://[::1]:8080/v1":                       "http://[::1]:8080",
		" model-name ":                               "model-name",
		" http://%invalid ":                          "http://%invalid",
	} {
		if got := backoffKey(raw); got != want {
			t.Errorf("backoffKey(%q)=%q, want %q", raw, got, want)
		}
	}
	var pool Pool
	pool.mu.Lock()
	first := pool.backoffFor("http://fixture.example/v1")
	alias := pool.backoffFor("http://FIXTURE.example/alternate")
	independent := pool.backoffFor("http://other.example/v1")
	pool.mu.Unlock()
	if first != alias || first == independent {
		t.Fatal("gate identity must follow server origin instead of model/provider aliases")
	}
	if first.initial != config.DefaultModelBackoffInitial || first.max != config.DefaultModelBackoffMax {
		t.Fatalf("gate does not use config backoff defaults: %+v", first)
	}
}

func TestBackoffAdmissionRecorderRetainsAgentAttribution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var records []CallRecord
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		r := &recordingModel{gate: gate, agentID: "worker", providerID: "fixture", model: "configured-model", rec: func(record CallRecord) {
			records = append(records, record)
		}}
		_, finish, err := r.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		finish(nil)
		if len(records) != 1 || records[0].Backoff == nil {
			t.Fatalf("expected one pre-request wait record: %+v", records)
		}
		record := records[0]
		if record.AgentID != "worker" || record.ProviderID != "fixture" || record.Model != "configured-model" || record.Backoff.Attempt != 1 || record.Backoff.WaitMS != 5000 {
			t.Fatalf("wait attribution/duration incorrect: %+v", record)
		}
		if gate.failures != 0 {
			t.Fatal("successful admitted probe did not reset outage")
		}
	})
}

func TestBackoffShortCooldownIsNotReportedAsAnUnknownProbeWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(500*time.Microsecond, time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		var wait *BackoffRecord
		r := &recordingModel{gate: gate, rec: func(record CallRecord) { wait = record.Backoff }}
		_, finish, err := r.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		finish(nil)
		if wait == nil || wait.WaitMS != 1 {
			t.Fatalf("a known positive cooldown must not use the unknown-probe sentinel: %+v", wait)
		}
	})
}

func TestBackoffCooldownIdentityIsSharedAcrossRolesButNotGenerationsOrOrigins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		var ids []string
		recordWait := func(gate *backoffGate, role string) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			r := &recordingModel{gate: gate, agentID: role, rec: func(record CallRecord) {
				ids = append(ids, record.Backoff.CooldownID)
			}}
			if _, _, err := r.admit(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected a canceled cooldown wait, got %v", err)
			}
		}
		recordWait(gate, "manager")
		recordWait(gate, "worker")
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		recordWait(gate, "manager")
		other := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, other, nil).finish(backoffFailure)
		recordWait(other, "manager")
		if len(ids) != 4 || ids[0] == "" || ids[0] != ids[1] || ids[0] == ids[2] || ids[0] == ids[3] {
			t.Fatalf("cooldown identities must follow the actual gate generation: %v", ids)
		}
	})
}

func TestBackoffAdmissionWithoutGateAndCanceledWait(t *testing.T) {
	ctx := context.Background()
	plain := &recordingModel{}
	got, finish, err := plain.admit(ctx)
	if err != nil || got != ctx || finish == nil {
		t.Fatalf("model without gate: ctx=%v err=%v", got, err)
	}
	finish(errors.New("ignored"))
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r := &recordingModel{gate: gate}
		_, finish, err := r.admit(ctx)
		if !errors.Is(err, context.DeadlineExceeded) || finish != nil || gate.failures != 1 {
			t.Fatalf("canceled admission must not acquire or reset gate: finishNil=%v err=%v failures=%d", finish == nil, err, gate.failures)
		}
	})
}

func TestBackoffAdmissionOutcomesAndReconnectShareFailureStreak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		r := &recordingModel{gate: gate}
		_, finish, err := r.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		finish(io.ErrUnexpectedEOF)
		if gate.failures != 1 {
			t.Fatal("model response failure did not start shared backoff")
		}
		ctx, finish, err := r.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := ctx.Value(reconnectBackoffKey{}).(func() error)(); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != 10*time.Second || gate.failures != 2 {
			t.Fatalf("fresh connection bypassed shared failure streak: elapsed=%s failures=%d", time.Since(start), gate.failures)
		}
		finish(&openai.APIError{HTTPStatusCode: 401, Message: "unauthorized"})
		if gate.failures != 2 || gate.probe || time.Until(gate.next) != 10*time.Second {
			t.Fatal("permanent rejection must release the probe without erasing the upstream failure streak")
		}
	})
}

func TestBackoffAdmissionCanceledCallsDoNotBlameServer(t *testing.T) {
	for _, activeCtxCancellation := range []bool{false, true} {
		for _, terminal := range []error{context.Canceled, context.DeadlineExceeded} {
			synctest.Test(t, func(t *testing.T) {
				gate := newBackoffGate(time.Second, 5*time.Second)
				r := &recordingModel{gate: gate}
				ctx, cancel := context.WithCancel(context.Background())
				_, finish, err := r.admit(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if activeCtxCancellation {
					cancel()
					finish(io.ErrUnexpectedEOF)
				} else {
					finish(fmt.Errorf("wrapped: %w", terminal))
				}
				cancel()
				if gate.failures != 0 {
					t.Fatal("canceled model call was counted as an upstream outage")
				}
			})
		}
	}
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		r := &recordingModel{gate: gate}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		ctx, finish, err := r.admit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := ctx.Value(reconnectBackoffKey{}).(func() error)(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("canceled reconnect returned %v", err)
		}
		finish(context.DeadlineExceeded)
		if gate.failures != 1 {
			t.Fatal("canceled reconnect changed the failure that triggered it")
		}
	})
}

// net/http's response-header timeout matches DeadlineExceeded while the
// caller's context is still live. That service failure must increase backoff.
func TestBackoffProviderDeadlineDoesNotMasqueradeAsCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		r := &recordingModel{gate: gate}
		ctx := context.Background()
		_, finish, err := r.admit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		providerErr := fmt.Errorf("model HTTP: %w", backoffProviderTimeout{})
		if !errors.Is(providerErr, context.DeadlineExceeded) || ctx.Err() != nil {
			t.Fatal("fixture must distinguish provider timeout from caller cancellation")
		}
		finish(providerErr)
		if gate.failures != 1 || time.Until(gate.next) != 5*time.Second {
			t.Fatalf("live-context service timeout did not start cooldown: failures=%d delay=%s", gate.failures, time.Until(gate.next))
		}
	})
}

type backoffProviderTimeout struct{}

func (backoffProviderTimeout) Error() string        { return "timeout awaiting response headers" }
func (backoffProviderTimeout) Timeout() bool        { return true }
func (backoffProviderTimeout) Temporary() bool      { return true }
func (backoffProviderTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

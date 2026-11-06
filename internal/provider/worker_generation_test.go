package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A resumed worker reuses its ID. Each request must retain the invocation's
// generation, including failed HTTP requests, rather than borrowing the final
// roster's generation when the telemetry callback runs.
func TestWorkerRequestsRecordTheirActualGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		generate   bool
		refuseHTTP bool
		otherAgent bool
	}{
		{name: "stream"},
		{name: "generate", generate: true},
		{name: "stream-http-error", refuseHTTP: true},
		{name: "generate-http-error", generate: true, refuseHTTP: true},
		{name: "different-record-owner", otherAgent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				if tc.refuseHTTP {
					http.Error(w, `{"error":{"message":"fixture refusal","type":"invalid_request_error"}}`, http.StatusUnauthorized)
					return
				}
				if body.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, backoffChunk+"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, backoffCompletion)
			}))
			t.Cleanup(endpoint.Close)
			pool := backoffPool(endpoint.URL)
			records := make(chan CallRecord, 4)
			build, err := pool.ModelBuilder(t.Context(), "fixture", "", "", func(record CallRecord) { records <- record })
			if err != nil {
				t.Fatal(err)
			}
			builder := func(role, id string) model.BaseChatModel {
				if tc.otherAgent {
					id = "independent-observer"
				}
				inner := build(role, id)
				if tc.generate {
					return generateWorkerModel{inner}
				}
				return inner
			}
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			worker, err := reg.Spawn(t.Context(), "reader", "Inspect the available evidence.", builder)
			if err != nil {
				t.Fatal(err)
			}
			for generation := 1; generation <= 2; generation++ {
				select {
				case <-worker.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("actual worker did not finish")
				}
				_, resultErr, done := worker.Result()
				if !done || (resultErr != nil) != tc.refuseHTTP {
					t.Fatalf("HTTP outcome changed: done=%v err=%v", done, resultErr)
				}
				var record CallRecord
				select {
				case record = <-records:
				case <-time.After(5 * time.Second):
					t.Fatal("actual HTTP request was not recorded")
				}
				want := generation
				if tc.otherAgent {
					want = 0
				}
				// Reflection keeps the pre-fix test executable: a missing field
				// is the semantic RED, not a compilation error.
				got := 0
				if field := reflect.ValueOf(record).FieldByName("WorkerGeneration"); field.IsValid() {
					got = int(field.Int())
				}
				if got != want {
					t.Errorf("HTTP request generation=%d want=%d, worker=%s record=%+v", got, want, worker.ID, record)
				}
				if generation == 1 {
					worker, err = reg.Resume(t.Context(), worker.ID, "Inspect the next evidence.", builder)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

// ADK drives streams; this fixture also exercises the provider's real Generate
// HTTP path under the same trusted Registry invocation context.
type generateWorkerModel struct{ model.BaseChatModel }

func (m generateWorkerModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{out}), nil
}

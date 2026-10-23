package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// A finished worker is still owed to the manager until the real wait tool
// returns its result. A host's Stats poll must not make that ID unknown.
func TestActualWaitCollectsACompletedWorkerAfterStatsPrunesIt(t *testing.T) {
	r := newSettlementHTTPRegistry(t, nil)
	id, handle := spawnSettlementWorker(t, r)
	select {
	case <-handle.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("offline worker did not finish")
	}
	r.Stats()
	pending := r.PendingAgents()
	if len(pending) != 1 || pending[0].AgentID != id || pending[0].Running || pending[0].Generation != 1 {
		t.Fatalf("completed result was not pending: %+v", pending)
	}
	out, err := invokable(t, r.Tools()[2]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":1}`, id))
	if err != nil {
		t.Fatal(err)
	}
	var report waitReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Agents) != 1 || report.Agents[0].Status != "done" || report.Agents[0].Result != "The assigned inspection finished." {
		t.Fatalf("Stats lost a paid-for uncollected result: %s", out)
	}
	if len(r.PendingAgents()) != 0 || report.Agents[0].Generation != 1 || !r.SnapshotWorkers()[0].Collected {
		t.Fatal("the actual returned result did not settle its exact generation")
	}
}

func newSettlementHTTPRegistry(t *testing.T, hold <-chan struct{}, entered ...chan struct{}) *Registry {
	t.Helper()
	shutdown := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if len(entered) != 0 {
			select {
			case entered[0] <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-req.Context().Done():
				return
			case <-shutdown:
				return
			}
		}
		fmt.Fprint(w, "data: {\"id\":\"offline-settlement\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"The assigned inspection finished.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"offline-settlement\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(endpoint.Close)
	t.Cleanup(func() { close(shutdown) })
	m, err := openaimodel.NewChatModel(context.Background(), &openaimodel.ChatModelConfig{BaseURL: endpoint.URL, Model: "offline-settlement", HTTPClient: endpoint.Client()})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	r.ModelBuilder = func(string, string) model.BaseChatModel { return m }
	t.Cleanup(r.Close)
	return r
}

func spawnSettlementWorker(t *testing.T, r *Registry) (string, *Handle) {
	t.Helper()
	out, err := invokable(t, r.Tools()[0]).InvokableRun(t.Context(), `{"role":"inspector","task":"inspect assigned work"}`)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil || reply.AgentID == "" {
		t.Fatalf("spawn did not return a real ID: %s %v", out, err)
	}
	h, ok := r.get(reply.AgentID)
	if !ok {
		t.Fatal("spawned ID is missing")
	}
	return reply.AgentID, h
}

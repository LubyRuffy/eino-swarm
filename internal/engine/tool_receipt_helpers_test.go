package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

type toolScopeWorkerID struct {
	AgentID    string `json:"agent_id"`
	Generation int    `json:"generation"`
}

type toolScopeWait struct {
	Agents []struct {
		toolScopeWorkerID
		Status string `json:"status"`
		Result string `json:"result"`
	} `json:"agents"`
	TimedOut bool `json:"timed_out"`
}

// awaitToolResult waits for the durable receipt of one tool call; a stored row
// is the only proof the call reached the event log.
func awaitToolResult(ctx context.Context, st *store.Store, callID string) (store.Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		var rows []store.Event
		err := st.DB().WithContext(ctx).Where("kind = ? AND tool_call_id = ?", "tool_result", callID).Limit(2).Find(&rows).Error
		switch {
		case err != nil:
			return store.Event{}, fmt.Errorf("read durable tool receipt: %w", err)
		case len(rows) == 1:
			return rows[0], nil
		case len(rows) > 1:
			return store.Event{}, fmt.Errorf("multiple durable tool receipts for %q", callID)
		}
		time.Sleep(time.Millisecond)
	}
}

// invokeRegistryTool runs one of the Registry's real lifecycle tools.
func invokeRegistryTool(t *testing.T, reg *swarm.Registry, name, args string) string {
	t.Helper()
	for _, candidate := range reg.Tools() {
		info, err := candidate.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != name {
			continue
		}
		out, err := candidate.(tool.InvokableTool).InvokableRun(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("missing actual Registry tool %s", name)
	return ""
}

package server_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// A failed snapshot query must remain an HTTP failure. An empty success body
// would tell the collaboration view that all previously recorded work vanished.
func TestNuwaSnapshotQueryFailureReturnsHTTPError(t *testing.T) {
	for _, table := range []string{"nuwa_nodes", "events"} {
		t.Run(table, func(t *testing.T) {
			h := newHarness(t)
			project := h.newProject(map[string]any{"name": "Snapshot read failure", "nuwa_enabled": true})
			id := project["id"].(string)
			failure := errors.New("fixture snapshot query unavailable")
			var fired atomic.Bool
			// This private Store owns the callback until Close. No callback
			// removal can race a late query from another server actor.
			if err := h.app.Store.DB().Callback().Query().Before("gorm:query").Register("nuwa:test:snapshot-read-failure", func(tx *gorm.DB) {
				if tx.Statement.Table == table && fired.CompareAndSwap(false, true) {
					_ = tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			body := h.json(http.MethodGet, "/api/projects/"+id+"/nuwa", nil, http.StatusInternalServerError)
			if !fired.Load() || body["error"] == nil || !strings.Contains(body["error"].(string), failure.Error()) {
				t.Fatalf("snapshot failure was lost: fired=%v response=%v", fired.Load(), body)
			}
			for _, key := range []string{"nodes", "workers", "revision"} {
				if _, exists := body[key]; exists {
					t.Fatalf("failed snapshot included success field %q: %v", key, body)
				}
			}
		})
	}
}

// Once the project is deleted the durable revision lookup must end this
// subscription, so EventSource cannot remain falsely connected to an old owner.
func TestNuwaProjectDeletionEndsItsOpenSSEConnection(t *testing.T) {
	h := newHarness(t)
	project := h.newProject(map[string]any{"name": "Deleted stream owner", "nuwa_enabled": true})
	id := project["id"].(string)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/projects/"+id+"/nuwa/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := h.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("subscription failed: status=%d headers=%v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	var initial strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		initial.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(initial.String(), "event: nuwa_updated") || !strings.Contains(initial.String(), id) {
		t.Fatalf("missing initial owner/revision notification: %s", initial.String())
	}
	h.json(http.MethodDelete, "/api/projects/"+id, nil, http.StatusNoContent)
	if next, err := reader.ReadByte(); !errors.Is(err, io.EOF) || ctx.Err() != nil {
		t.Fatalf("deleted project kept its SSE subscription: next=%q error=%v context=%v", next, err, ctx.Err())
	}
}

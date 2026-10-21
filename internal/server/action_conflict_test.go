package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

// A vanished queue row is distinct from a live action whose prerequisite
// changed. Clients rely on these conflicts to recover without resending text.
func TestActionConflictsRemainDistinctFromMissingRecords(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no_steer", engine.ErrNoPendingSteer},
		{"ask_mismatch", engine.ErrAskMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			s := &Server{}
			router.POST("/action", func(c *gin.Context) { s.fail(c, fmt.Errorf("action: %w", tc.err)) })
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/action", nil))
			var body map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if res.Code != http.StatusConflict || body["code"] != tc.name || body["request_id"] != nil {
				t.Fatalf("recoverable conflict was misclassified: status=%d body=%v", res.Code, body)
			}
		})
	}
}

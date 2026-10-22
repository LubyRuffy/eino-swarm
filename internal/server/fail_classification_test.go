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

// Launchers retry stopping engines; conversation conflicts need user action.
// Wrapping either cause must retain that distinction and its error context.
func TestFailureClassificationPreservesWrappedLifecycleErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		cause  error
		status int
		code   string
	}{
		{ErrEngineStopping, http.StatusServiceUnavailable, "engine_stopping"},
		{engine.ErrNoPendingSteer, http.StatusConflict, "no_steer"},
		{engine.ErrAskMismatch, http.StatusConflict, "ask_mismatch"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			err := fmt.Errorf("request rejected: %w", tc.cause)
			(&Server{}).fail(ctx, err)
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if decodeErr := json.Unmarshal(rec.Body.Bytes(), &body); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if rec.Code != tc.status || body.Code != tc.code || body.Error != err.Error() {
				t.Fatalf("lost lifecycle category or context: status=%d body=%+v", rec.Code, body)
			}
		})
	}
}

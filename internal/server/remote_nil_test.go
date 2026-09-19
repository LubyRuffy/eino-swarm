package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRemoteHandlersWithoutAHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/remote/status", nil)
	s.getRemoteStatus(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"online":false`) {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/remote/offer", nil)
	s.postRemoteOffer(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("offer %d", w.Code)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/remote/bindings", nil)
	s.listRemoteBindings(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "bindings") {
		t.Fatalf("bindings %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/remote/bindings/x/revoke", nil)
	s.revokeRemoteBinding(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("revoke %d", w.Code)
	}
}

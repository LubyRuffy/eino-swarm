package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/update"
	"github.com/gin-gonic/gin"
)

type updateStub struct {
	result update.Result
	err    error
	pids   []int
}

func (s *updateStub) Check(context.Context, bool) update.Result { return s.result }

func (s *updateStub) Apply(_ context.Context, _ string, pids []int) error {
	s.pids = append([]int(nil), pids...)
	return s.err
}

func TestUpdateCheckAndInstall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &updateStub{result: update.Result{
		Status: "available",
		Offer:  &update.Offer{Version: "1.2.4"},
	}}
	s := &Server{presence: newPresence(), updater: stub}
	id, err := s.presence.reserve("desktop", 4242)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.presence.attach(id); !ok {
		t.Fatal("attach")
	}
	r := gin.New()
	r.GET("/api/update", s.getUpdate)
	r.POST("/api/update", s.postUpdate)

	req := httptest.NewRequest(http.MethodGet, "/api/update?fresh=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available"`) {
		t.Fatalf("check %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{"version":"1.2.4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "restarting") {
		t.Fatalf("apply %d %s", rec.Code, rec.Body.String())
	}
	if len(stub.pids) != 1 || stub.pids[0] != 4242 {
		t.Fatalf("pids %v", stub.pids)
	}
	stub.err = http.ErrServerClosed
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{"version":"1.2.4"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("failed apply %d", rec.Code)
	}
}

func TestUpdateWithoutAFeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{presence: newPresence()}
	r := gin.New()
	r.GET("/api/update", s.getUpdate)
	r.POST("/api/update", s.postUpdate)
	req := httptest.NewRequest(http.MethodGet, "/api/update", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "unsupported") {
		t.Fatal(rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{"version":"1.2.4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.updater = &updateStub{}
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body %d %s", rec.Code, rec.Body.String())
	}
}

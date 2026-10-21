package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/update"
	"github.com/gin-gonic/gin"
)

type updateStub struct {
	result update.Result
	err    error
	pids   []int
}

func TestPresenceRejectsPIDValuesThatCouldBecomeProcessGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{presence: newPresence()}
	r := gin.New()
	r.POST("/api/presence", s.postPresence)
	for _, pid := range []string{"-1", "2147483648", "4294967295"} {
		req := httptest.NewRequest(http.MethodPost, "/api/presence", strings.NewReader(`{"surface":"desktop","pid":`+pid+`}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("unsafe native PID %s was registered: %d %s", pid, rec.Code, rec.Body.String())
		}
	}
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
	r := gin.New()
	r.GET("/api/update", s.getUpdate)
	r.POST("/api/update", s.postUpdate)
	r.POST("/api/presence", s.postPresence)
	// Use the same body the native window sends; a manually seeded slot hid
	// a missing PID in the real launcher-to-presence chain.
	req := httptest.NewRequest(http.MethodPost, "/api/presence", strings.NewReader(`{"surface":"desktop","pid":4242}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var reserved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reserved); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("presence %d %s: %v", rec.Code, rec.Body.String(), err)
	}
	if _, release, ok := s.presence.attach(reserved.ID); !ok {
		t.Fatal("attach")
	} else {
		defer release()
	}

	req = httptest.NewRequest(http.MethodGet, "/api/update?fresh=1", nil)
	rec = httptest.NewRecorder()
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
	stub.err = fmt.Errorf("install: %w", update.ErrBusy)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{"version":"1.2.4"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"update_busy"`) {
		t.Fatalf("concurrent install %d %s", rec.Code, rec.Body.String())
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

type updateProgressStub struct {
	updateStub
	progress update.Progress
	apply    func(context.Context, string, []int) error
}

func (s *updateProgressStub) Progress() update.Progress { return s.progress }
func (s *updateProgressStub) Apply(ctx context.Context, version string, pids []int) error {
	if s.apply != nil {
		return s.apply(ctx, version, pids)
	}
	return s.updateStub.Apply(ctx, version, pids)
}

func TestUpdateProgressIsReadableWhileTheInstallRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started, release := make(chan struct{}), make(chan struct{})
	stub := &updateProgressStub{progress: update.Progress{Stage: "downloading", Version: "1.2.4", DownloadedBytes: 7, TotalBytes: 20}, apply: func(context.Context, string, []int) error { close(started); <-release; return nil }}
	s := &Server{presence: newPresence(), updater: stub}
	r := gin.New()
	r.GET("/api/update/progress", s.getUpdateProgress)
	r.POST("/api/update", s.postUpdate)
	ts := httptest.NewServer(r)
	defer ts.Close()
	type applyReply struct {
		response *http.Response
		err      error
	}
	posted := make(chan applyReply, 1)
	go func() {
		res, err := ts.Client().Post(ts.URL+"/api/update", "application/json", strings.NewReader(`{"version":"1.2.4"}`))
		posted <- applyReply{res, err}
	}()
	defer func() {
		close(release)
		reply := <-posted
		if reply.err != nil {
			t.Errorf("apply failed: %v", reply.err)
			return
		}
		defer reply.response.Body.Close()
		if reply.response.StatusCode != http.StatusOK {
			t.Errorf("apply status=%d", reply.response.StatusCode)
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("install request did not reach Apply")
	}
	res, err := ts.Client().Get(ts.URL + "/api/update/progress")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got update.Progress
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || got != stub.progress {
		t.Fatalf("progress status=%d body=%+v", res.StatusCode, got)
	}
}

func TestUpdateProgressWithoutAReporterIsIdle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, updater := range []update.Checker{nil, &updateStub{}} {
		s := &Server{updater: updater}
		r := gin.New()
		r.GET("/api/update/progress", s.getUpdateProgress)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/update/progress", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"stage":"idle","downloaded_bytes":0,"total_bytes":0}` {
			t.Fatalf("unsupported progress=%d %s", rec.Code, rec.Body.String())
		}
	}
}

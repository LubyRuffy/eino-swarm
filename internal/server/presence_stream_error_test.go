package server

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// A failed keepalive write must release native presence even before request
// cancellation arrives, otherwise the lost window would pin the engine.
func TestPresenceWriteFailureReleasesTheLostNativeClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := newPresence()
	id, err := p.reserve("desktop", 7)
	if err != nil {
		t.Fatal(err)
	}
	w := &closedPresenceWriter{ResponseRecorder: httptest.NewRecorder(), held: make(chan struct{})}
	ctx, _ := gin.CreateTestContext(w)
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/presence/"+id, nil).WithContext(requestCtx)
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		(&Server{presence: p}).holdPresence(ctx)
	}()
	select {
	case <-w.held:
	case <-time.After(time.Second):
		t.Fatal("native presence was not acknowledged")
	}
	if clients := p.list(); len(clients) != 1 || clients[0].ID != id {
		t.Fatalf("acknowledged native client was not counted: %+v", clients)
	}
	select {
	case <-done:
	case <-time.After(heartbeatInterval + 5*time.Second):
		t.Fatal("keepalive write failure did not end the presence hold")
	}
	if !w.failed || len(p.list()) != 0 {
		t.Fatalf("lost client still pins the engine: writeFailed=%v clients=%+v", w.failed, p.list())
	}
}

type closedPresenceWriter struct {
	*httptest.ResponseRecorder
	held   chan struct{}
	failed bool
}

func (w *closedPresenceWriter) Write(body []byte) (int, error) {
	if bytes.Equal(body, []byte(": ping\n\n")) {
		w.failed = true
		return 0, net.ErrClosed
	}
	n, err := w.ResponseRecorder.Write(body)
	close(w.held)
	return n, err
}

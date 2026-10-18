package remote

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

func catalogPump(t *testing.T, e *engine.Engine) (*LinkPump, <-chan Response) {
	t.Helper()
	replies := make(chan Response, 32)
	p := &LinkPump{eng: e, path: "relay", sessionID: "s", send: func(raw []byte) error {
		var resp Response
		if err := json.Unmarshal(raw, &resp); err != nil {
			return err
		}
		replies <- resp
		return nil
	}}
	t.Cleanup(p.Close)
	return p, replies
}

func dispatchCatalog(p *LinkPump, req Request) {
	raw, _ := json.Marshal(req)
	p.Dispatch(raw)
}

func takeCatalogReply(t *testing.T, replies <-chan Response) Response {
	t.Helper()
	select {
	case r := <-replies:
		return r
	case <-time.After(time.Second):
		t.Fatal("model discovery blocked the phone's RPC receive loop")
		return Response{}
	}
}

func TestCatalogRefreshDoesNotBlockOtherRPCsAndAllowsOnlyOneDiscovery(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"data":[{"id":"fresh"}]}`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	e := catalogEngine(t, config.Provider{ID: "endpoint", BaseURL: server.URL, Model: "old"})
	p, replies := catalogPump(t, e)
	defer p.Close()
	defer once.Do(func() { close(release) })
	dispatchCatalog(p, Request{ID: "refresh", Op: OpRefreshCatalog})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("discovery did not reach the offline HTTP adapter")
	}
	dispatchCatalog(p, Request{ID: "catalog", Op: OpCatalog})
	if r := takeCatalogReply(t, replies); r.ID != "catalog" || !r.OK || r.Models[0].Model != "old" {
		t.Fatalf("ordinary catalog during discovery=%+v", r)
	}
	dispatchCatalog(p, Request{ID: "stop", Op: OpStop, ThreadID: "not-running"})
	if r := takeCatalogReply(t, replies); r.ID != "stop" {
		t.Fatalf("Stop was held behind discovery: %+v", r)
	}
	dispatchCatalog(p, Request{ID: "duplicate", Op: OpRefreshCatalog})
	if r := takeCatalogReply(t, replies); r.ID != "duplicate" || r.OK || r.Code != "busy" {
		t.Fatalf("parallel discovery was not refused: %+v", r)
	}
	dispatchCatalog(p, Request{V: ProtocolV + 1, ID: "bad-version", Op: OpRefreshCatalog})
	if r := takeCatalogReply(t, replies); r.Code != "bad_version" {
		t.Fatalf("new async path bypassed the protocol version: %+v", r)
	}
	once.Do(func() { close(release) })
	if r := takeCatalogReply(t, replies); r.ID != "refresh" || !r.OK || len(r.Models) != 1 || r.Models[0].Model != "fresh" || requests.Load() != 1 {
		t.Fatalf("discovery reply=%+v requests=%d", r, requests.Load())
	}
}

func TestCatalogRefreshCloseCancelsHTTPAndJoinsWithoutLateReply(t *testing.T) {
	entered := make(chan struct{})
	left := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(left)
	}))
	defer server.Close()
	e := catalogEngine(t, config.Provider{ID: "endpoint", BaseURL: server.URL, Model: "old"})
	p, replies := catalogPump(t, e)
	defer p.Close()
	dispatchCatalog(p, Request{ID: "refresh", Op: OpRefreshCatalog})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("discovery did not start")
	}
	done := make(chan struct{})
	go func() { p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join discovery")
	}
	select {
	case <-left:
	case <-time.After(time.Second):
		t.Fatal("HTTP adapter did not receive cancellation")
	}
	dispatchCatalog(p, Request{ID: "after-close", Op: OpRefreshCatalog})
	if len(replies) != 0 {
		t.Fatalf("closed link got a late response: %+v", <-replies)
	}
}

func TestCatalogRefreshConcurrentCloseDoesNotAdmitNewJobs(t *testing.T) {
	e := testEngine(t)
	p, _ := catalogPump(t, e)
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				p.Close()
				return
			}
			dispatchCatalog(p, Request{ID: "r", Op: OpRefreshCatalog})
		}()
	}
	wg.Wait()
	p.Close()
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	if !p.catalogClosed || p.catalogCancel != nil {
		t.Fatal("closed link retained a model discovery job")
	}
}

// The pending job owns both discovery and its response. Releasing it before
// the write returns lets a slow socket accumulate another refresh per request.
func TestCatalogRefreshKeepsSlotUntilReplyReturns(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	e := testEngine(t)
	p := &LinkPump{eng: e, path: "relay", sessionID: "s", send: func([]byte) error {
		entered <- struct{}{}
		<-release
		return nil
	}}
	dispatchCatalog(p, Request{ID: "refresh", Op: OpRefreshCatalog})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reply did not reach the controlled socket write")
	}
	p.catalogMu.Lock()
	owned := p.catalogCancel != nil
	p.catalogMu.Unlock()
	done := make(chan struct{})
	go func() { p.Close(); close(done) }()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not join the completed response")
	}
	if !owned {
		t.Fatal("refresh slot released while reply was blocked; another request can launch another worker")
	}
}

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPhoneSelectionTranslationDoesNotCreateATurn(t *testing.T) {
	e := testEngine(t)
	if err := e.Config().Update(func(c *config.Config) error { c.Swarm.SelectionTranslation = true; return nil }); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "tr", Op: "translate_selection", ThreadID: thread.ID, Text: "Keep two hosts."}, "relay", "session")
	raw, _ := json.Marshal(resp)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if !resp.OK || fields["translation"] == nil {
		t.Fatalf("translation: %s", raw)
	}
	turns, err := e.Store().ListTurns(thread.ID)
	if err != nil || len(turns) != 0 {
		t.Fatalf("translation created turns: %v, %v", turns, err)
	}
}

// The real provider boundary must stay tool-free and leave conversation history alone.
func TestPhoneSelectionUsesPCModelAndBoundsPrivateFailures(t *testing.T) {
	for _, tc := range []struct {
		name, output, code string
		status             int
	}{
		{name: "success", output: "保留两台主机。", status: 200},
		{name: "escaped frame", output: strings.Repeat("<", 12000), status: 200, code: "selection_translation_too_long"},
		{name: "empty", status: 200, code: "selection_translation_unavailable"},
		{name: "upstream secret", status: 401, code: "selection_translation_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string                           `json:"model"`
					Tools    []any                            `json:"tools"`
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Model != "phone-fixture" || len(request.Tools) != 0 || len(request.Messages) != 2 || request.Messages[1].Content != "Keep two hosts." {
					t.Errorf("unexpected model request: %+v", request)
				}
				if tc.status != 200 {
					http.Error(w, "private-upstream-key", tc.status)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": tc.output}, "finish_reason": "stop"}}})
			}))
			defer server.Close()
			e := catalogEngine(t, config.Provider{ID: "translation-fixture", Model: "phone-fixture", BaseURL: server.URL, APIMode: config.APIModeChatCompletions})
			if err := e.Config().Update(func(c *config.Config) error { c.Swarm.SelectionTranslation = true; return nil }); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("translation", "", "")
			if err != nil {
				t.Fatal(err)
			}
			resp := Handle(e, config.RemoteConfig{}, Request{ID: "tr", Op: OpTranslateSelection, ThreadID: thread.ID, Text: "Keep two hosts."}, "relay", "s")
			if resp.Code != tc.code || resp.OK != (tc.code == "") {
				t.Fatalf("response: %+v", resp)
			}
			if tc.code == "" && resp.Translation != tc.output {
				t.Fatalf("translation=%q", resp.Translation)
			}
			raw, _ := json.Marshal(resp)
			if strings.Contains(string(raw), "private-upstream-key") || strings.Contains(string(raw), server.URL) || len(raw) > plaintextBudget() {
				t.Fatal("unsafe response")
			}
			turns, err := e.Store().ListTurns(thread.ID)
			if err != nil || len(turns) != 0 {
				t.Fatalf("translation changed history: %v %v", turns, err)
			}
		})
	}
}

func TestPhoneSelectionValidationAndCancellation(t *testing.T) {
	e := testEngine(t)
	if err := e.Config().Update(func(c *config.Config) error { c.Swarm.SelectionTranslation = true; return nil }); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		req  Request
		code string
	}{
		{Request{V: ProtocolV + 1, Op: OpTranslateSelection}, "bad_version"},
		{Request{Op: OpTranslateSelection, ThreadID: thread.ID}, "bad_request"},
		{Request{Op: OpTranslateSelection, ThreadID: thread.ID, Text: strings.Repeat("字", 8001)}, "selection_translation_too_long"},
		{Request{Op: OpTranslateSelection, ThreadID: "missing", Text: "Text"}, "not_found"},
	} {
		if r := Handle(e, config.RemoteConfig{}, tc.req, "relay", "s"); r.Code != tc.code {
			t.Fatalf("got %+v want %s", r, tc.code)
		}
	}
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		code := "selection_translation_cancelled"
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			code = "selection_translation_timeout"
		} else {
			cancel()
		}
		resp := handleSelectionTranslation(ctx, e, Request{ID: "cancelled", ThreadID: thread.ID, Text: "Text"}, "relay", "s")
		cancel()
		if resp.Code != code {
			t.Fatalf("context failure: %+v", resp)
		}
	}
	if r := handleSelectionTranslation(context.Background(), e, Request{V: ProtocolV + 1}, "relay", "s"); r.Code != "bad_version" {
		t.Fatal(r)
	}
	e.BeginRestore()
	if r := handleSelectionTranslation(context.Background(), e, Request{Text: "Text"}, "relay", "s"); r.OK || r.Code != "restoring" {
		t.Fatal(r)
	}
	if r := Handle(e, config.RemoteConfig{}, Request{Op: OpTranslateSelection, Text: "Text"}, "relay", "s"); r.OK || r.Code != "restoring" {
		t.Fatalf("startup admission: %+v", r)
	}
}

func TestPhoneSelectionDoesNotBlockStopAndOnlyMatchingCancelStopsUpstream(t *testing.T) {
	for _, closeLink := range []bool{false, true} {
		t.Run(fmt.Sprint("close=", closeLink), func(t *testing.T) {
			entered, left := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(entered)
				<-r.Context().Done()
				close(left)
			}))
			defer server.Close()
			e := catalogEngine(t, config.Provider{ID: "fixture", Model: "fixture", BaseURL: server.URL, APIMode: config.APIModeChatCompletions})
			if err := e.Config().Update(func(c *config.Config) error { c.Swarm.SelectionTranslation = true; return nil }); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("translation", "", "")
			if err != nil {
				t.Fatal(err)
			}
			p, replies := catalogPump(t, e)
			for _, id := range []string{"", strings.Repeat("x", 257)} {
				dispatchCatalog(p, Request{ID: id, Op: OpTranslateSelection})
				if r := takeCatalogReply(t, replies); r.Code != "bad_request" {
					t.Fatalf("bad id: %+v", r)
				}
			}
			dispatchCatalog(p, Request{ID: "translation", Op: OpTranslateSelection, ThreadID: thread.ID, Text: "Keep two hosts."})
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("translation did not reach model")
			}
			dispatchCatalog(p, Request{ID: "stop", Op: OpStop, ThreadID: thread.ID})
			if r := takeCatalogReply(t, replies); r.ID != "stop" {
				t.Fatalf("Stop blocked: %+v", r)
			}
			dispatchCatalog(p, Request{ID: "other", Op: OpTranslateSelection})
			if r := takeCatalogReply(t, replies); r.Code != "selection_translation_busy" {
				t.Fatalf("parallel call: %+v", r)
			}
			dispatchCatalog(p, Request{ID: "wrong-cancel", Op: OpCancelTranslation, CallID: "other"})
			if r := takeCatalogReply(t, replies); !r.OK {
				t.Fatal(r)
			}
			select {
			case <-left:
				t.Fatal("wrong cancellation stopped the model")
			default:
			}
			if closeLink {
				p.Close()
			} else {
				dispatchCatalog(p, Request{ID: "cancel", Op: OpCancelTranslation, CallID: "translation"})
				found := map[string]Response{}
				for range 2 {
					r := takeCatalogReply(t, replies)
					found[r.ID] = r
				}
				if !found["cancel"].OK || found["translation"].Code != "selection_translation_cancelled" {
					t.Fatalf("cancel replies: %+v", found)
				}
			}
			select {
			case <-left:
			case <-time.After(time.Second):
				t.Fatal("HTTP model request survived cancellation")
			}
			p.Close()
			dispatchCatalog(p, Request{ID: "closed", Op: OpTranslateSelection})
			select {
			case r := <-replies:
				t.Fatalf("late reply after Close: %+v", r)
			default:
			}
		})
	}
}

func TestPhoneSelectionRequiresWorkAuthorization(t *testing.T) {
	p, replies := catalogPump(t, testEngine(t))
	p.allow = func() (bool, bool) { return false, false }
	dispatchCatalog(p, Request{ID: "refuse", Op: OpTranslateSelection})
	if r := takeCatalogReply(t, replies); r.Code != "login_required" {
		t.Fatalf("work admission: %+v", r)
	}
	dispatchCatalog(p, Request{ID: "cancel", Op: OpCancelTranslation})
	if r := takeCatalogReply(t, replies); !r.OK {
		t.Fatalf("cancellation should remain available: %+v", r)
	}
}

func TestPairedPhoneCannotTranslateAfterTheHostSwitchesItOff(t *testing.T) {
	e := testEngine(t)
	thread, err := e.CreateThread("Read a quote", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Op: OpTranslateSelection, ThreadID: thread.ID, Text: "Selected text."}
	if resp := Handle(e, config.RemoteConfig{}, req, "relay", "session"); resp.OK || resp.Code != "selection_translation_disabled" {
		t.Fatalf("disabled RPC: %+v", resp)
	}
}

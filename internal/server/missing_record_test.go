package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// A stale queue action must identify the vanished row without looking like a
// failed model run, accepting it again, or losing the request's diagnostic ID.
func TestMissingQueuedMessageIdentifiesTheRequestAndRecord(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	var logs bytes.Buffer
	s, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		thread, followup, code string
	}{
		{id, "fu_gone", "followup_unavailable"},
		{"th_gone", "fu_gone", "thread_not_found"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			logs.Reset()
			path := "/api/threads/" + tc.thread + "/followups/" + tc.followup + "/steer"
			req := httptest.NewRequest(http.MethodPost, path+"?secret=do-not-log", strings.NewReader("private-body"))
			req.Header.Set("Authorization", "Bearer do-not-log")
			res := httptest.NewRecorder()
			s.Handler().ServeHTTP(res, req)
			var body map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if res.Code != http.StatusNotFound || body["code"] != tc.code || body["thread_id"] != tc.thread {
				t.Fatalf("missing resource lost its identity: status=%d body=%v", res.Code, body)
			}
			if tc.code == "followup_unavailable" && body["followup_id"] != tc.followup {
				t.Fatalf("missing follow-up ID: %v", body)
			}
			requestID, _ := body["request_id"].(string)
			if !strings.HasPrefix(requestID, "req_") || res.Header().Get("X-Request-ID") != requestID {
				t.Fatalf("missing request correlation: header=%q body=%v", res.Header().Get("X-Request-ID"), body)
			}
			if body["method"] != http.MethodPost || body["route"] != "/api/threads/:id/followups/:fid/steer" {
				t.Fatalf("missing operation: %v", body)
			}
			if strings.Contains(body["error"].(string), "store:") {
				t.Fatalf("internal sentinel reached the user: %v", body)
			}
			var logged map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &logged); err != nil {
				t.Fatalf("no diagnostic at the default log level: %s (%v)", logs.String(), err)
			}
			if logged["request_id"] != requestID || logged["code"] != tc.code || logged["thread_id"] != tc.thread {
				t.Fatalf("log cannot locate this failure: %v", logged)
			}
			for _, secret := range []string{"do-not-log", "private-body", "secret="} {
				if strings.Contains(logs.String()+res.Body.String(), secret) {
					t.Fatalf("request diagnostics disclosed %q", secret)
				}
			}
		})
	}
}

func TestRepeatedQueuedSteerDoesNotStartAnotherTurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	f, err := h.app.Store.EnqueueFollowup(id, "Process the next item")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/threads/" + id + "/followups/" + f.ID + "/steer"
	first := h.json(http.MethodPost, path, nil, http.StatusAccepted)
	if first["turn"] == nil {
		t.Fatalf("idle queue action did not start its turn: %v", first)
	}
	second := h.json(http.MethodPost, path, nil, http.StatusNotFound)
	if second["code"] != "followup_unavailable" || second["followup_id"] != f.ID {
		t.Fatalf("repeated request did not explain the vanished queue row: %v", second)
	}
	turns, err := h.app.Store.ListTurns(id)
	if err != nil || len(turns) != 1 {
		t.Fatalf("same queue row started twice: %v err=%v", turns, err)
	}
}

func TestMissingRecordFallbackRetainsTheOperation(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodGet, "/api/projects/pj_gone", nil)
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound || body["code"] != "not_found" ||
		body["record_id"] != "pj_gone" || body["method"] != http.MethodGet || body["route"] != "/api/projects/:id" {
		t.Fatalf("generic missing record lost operation context: status=%d body=%v", res.StatusCode, body)
	}
	if body["request_id"] != res.Header.Get("X-Request-ID") || body["error"] == "store: not found" {
		t.Fatalf("generic missing record is opaque: %v", body)
	}
}

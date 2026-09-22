package tui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenThreadReusesTheLatestConversation(t *testing.T) {
	var created int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/threads":
			_, _ = io.WriteString(w, `{"threads":[{"id":"th_existing"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/threads":
			created++
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"thread":{"id":"th_new"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	id, err := c.OpenThread(context.Background(), ClientConfig{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "th_existing" || created != 0 {
		t.Fatalf("interactive attach created a conversation: id=%s created=%d", id, created)
	}
	id, err = c.OpenThread(context.Background(), ClientConfig{Task: "do the work"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "th_new" || created != 1 {
		t.Fatalf("a task must be its own conversation: id=%s created=%d", id, created)
	}
}

func TestSendWhileBusyBecomesAFollowUp(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		switch {
		case strings.HasSuffix(r.URL.Path, "/turns"):
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"busy","code":"busy"}`)
		case strings.HasSuffix(r.URL.Path, "/followups"):
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/answers"):
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.Send(context.Background(), "th", "next", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/followups") {
		t.Fatalf("busy turn was not queued: %s", path)
	}
	if err := c.Send(context.Background(), "th", "the reply", true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/answers") {
		t.Fatalf("an open question was sent as a turn: %s", path)
	}
}

func TestModelCommandPatchesTheThread(t *testing.T) {
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.Send(context.Background(), "th", "/model beta", false); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || !strings.HasSuffix(path, "/threads/th") || !strings.Contains(body, `"model":"beta"`) {
		t.Fatalf("model command method=%s path=%s body=%s", method, path, body)
	}
	if err := c.Send(context.Background(), "th", "/goal standing", false); err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPatch {
		t.Fatal("/goal must stay a turn, not a model patch")
	}
}

func TestWireMessagesKeepsUserLinesOffTheAgentStream(t *testing.T) {
	user := wireMessages("user_message", `{"text":"hello"}`)
	if len(user) != 1 || user[0].userText != "hello" || user[0].AgentID != "" {
		t.Fatalf("user line=%+v", user)
	}
	done := wireMessages("done", `{"agent_id":"manager","text":"finished"}`)
	if len(done) != 2 || !done[1].idle || done[0].Text != "finished" {
		t.Fatalf("done=%+v", done)
	}
	if got := wireMessages("ping", `{}`); got != nil {
		t.Fatalf("ping=%+v", got)
	}
}

func TestFirstSendPrefersTheTask(t *testing.T) {
	if got := firstSend(ClientConfig{Task: "work", Goal: "standing"}); got != "work" {
		t.Fatalf("task+goal sent %q", got)
	}
	if got := firstSend(ClientConfig{Goal: "standing"}); got != "/goal standing" {
		t.Fatalf("goal alone sent %q", got)
	}
	if got := firstSend(ClientConfig{}); got != "" {
		t.Fatalf("empty sent %q", got)
	}
}

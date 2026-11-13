package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecidePostsTheFixedQuestionAndFormatsTheAnswer(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/decisions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[{"name":"proceed","probability":0.8}]}`))
	}))
	defer srv.Close()

	p := New(configFor(t, true))
	src := accountSourceFor(t, srv.URL, "secret-token", "conversation")
	p.SetAccountSource(src)
	got, err := p.Decide(context.Background(), src.ID(), "decisions", "the request")
	if err != nil {
		t.Fatal(err)
	}
	if got != "proceed 0.80" {
		t.Fatalf("answer %q", got)
	}
	if !strings.Contains(body, `"model":"decisions"`) || !strings.Contains(body, `"name":"proceed"`) || !strings.Contains(body, "the request") {
		t.Fatalf("body %s", body)
	}
}

func TestDecideRejectsAnEmptyAnswer(t *testing.T) {
	if _, err := formatDecision([]byte(`{"answers":[]}`)); err == nil {
		t.Fatal("empty answers were accepted")
	}
	if _, err := formatDecision([]byte(`not json`)); err == nil {
		t.Fatal("malformed body was accepted")
	}
	got, err := formatDecision([]byte(`{"answers":[{"probability":1}]}`))
	if err != nil || got != "proceed 1.00" {
		t.Fatalf("blank name = %q %v", got, err)
	}
}

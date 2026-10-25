package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Recovery needs the failed predicate, without turning a peer URL or response
// body into a diagnostic. All failures still return the unverified record.
func TestDiscoverClassifiesUnreachableHealthWithoutLeakingPeerData(t *testing.T) {
	for _, test := range []struct {
		name     string
		category string
		status   int
		body     string
		closed   bool
		truncate bool
		redirect bool
	}{
		{name: "connection refused", category: "transport", closed: true},
		{name: "redirect limit", category: "transport", status: http.StatusFound, redirect: true},
		{name: "server error", category: "status", status: http.StatusInternalServerError, body: "private-response-marker"},
		{name: "other non-200", category: "status", status: http.StatusNoContent},
		{name: "truncated body", category: "read", status: http.StatusOK, body: "private-response-marker", truncate: true},
		{name: "HTML", category: "decode", status: http.StatusOK, body: "<html>private-response-marker</html>"},
		{name: "invalid JSON", category: "decode", status: http.StatusOK, body: `{"private-response-marker":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.redirect {
					http.Redirect(w, r, r.URL.String(), http.StatusFound)
					return
				}
				if test.truncate {
					w.Header().Set("Content-Length", strconv.Itoa(len(test.body)+100))
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			if test.closed {
				server.Close()
			}
			record := Record{PID: os.Getpid(), URL: server.URL + "?token=private-query-marker", Mock: true, Version: "unverified", Dev: 7, Ino: 9}
			if err := Publish(dir, record); err != nil {
				t.Fatal(err)
			}
			got, err := Discover(dir, false)
			if !errors.Is(err, ErrUnreachable) || got != record {
				t.Fatalf("failure must retain an unverified record: got=%+v err=%v", got, err)
			}
			if !errors.Is(fmt.Errorf("recovery: %w", err), ErrUnreachable) {
				t.Fatal("wrapping lost the original unreachable category")
			}
			var diagnostic *UnreachableError
			if !errors.As(err, &diagnostic) || diagnostic.Category != test.category || diagnostic.StatusCode != test.status || diagnostic.Duration <= 0 || diagnostic.ContextCategory != "" {
				t.Fatalf("typed failure predicate differs: %#v", err)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrMockMismatch) {
				t.Fatal("non-context health failure acquired a different classification")
			}
			message := err.Error()
			for _, field := range []string{"category=" + test.category, "status=" + strconv.Itoa(test.status), "elapsed_ms="} {
				if !strings.Contains(message, field) {
					t.Errorf("missing safe field %q in %q", field, message)
				}
			}
			for _, secret := range []string{server.URL, "private-query-marker", "private-response-marker"} {
				if strings.Contains(message, secret) {
					t.Errorf("diagnostic exposed peer data %q", secret)
				}
			}
		})
	}
}

func TestDiscoverKeepsTheHealthTimeoutForHeadersAndBody(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(fmt.Sprintf("headers=%t", headers), func(t *testing.T) {
			dir := t.TempDir()
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if headers {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			record := Record{PID: os.Getpid(), URL: server.URL + "?token=private-timeout-marker"}
			if err := Publish(dir, record); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			got, err := Discover(dir, false)
			elapsed := time.Since(started)
			if !errors.Is(err, ErrUnreachable) || got != record {
				t.Fatalf("timeout lost original record/category: got=%+v err=%v", got, err)
			}
			if elapsed < 450*time.Millisecond || elapsed > 2*time.Second {
				t.Fatalf("original 500ms client budget changed: %v", elapsed)
			}
			category := "transport"
			if headers {
				category = "read"
			}
			if !strings.Contains(err.Error(), "category="+category) || !strings.Contains(err.Error(), "context=deadline_exceeded") {
				t.Errorf("timeout predicate/context missing: %v", err)
			}
			var diagnostic *UnreachableError
			if !errors.As(err, &diagnostic) || diagnostic.Duration < 450*time.Millisecond || diagnostic.Duration > elapsed || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				t.Fatalf("typed timeout classification or duration differs: %#v", err)
			}
			if strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "private-timeout-marker") {
				t.Fatal("timeout diagnostic exposed the request URL")
			}
		})
	}
}

func TestUnreachableErrorRetainsOnlySafeContextCauses(t *testing.T) {
	for _, test := range []struct {
		cause error
		kind  string
	}{
		{cause: context.Canceled, kind: "canceled"},
		{cause: context.DeadlineExceeded, kind: "deadline_exceeded"},
		{cause: errors.New("private-underlying-marker")},
	} {
		cause := &url.Error{Op: "private-operation-marker", URL: "http://private-url-marker?token=private-query-marker", Err: fmt.Errorf("private-body-marker: %w", test.cause)}
		err := newUnreachableError("transport", 0, time.Now().Add(-time.Millisecond), cause)
		if err.ContextCategory != test.kind || err.Duration < time.Millisecond || !errors.Is(err, ErrUnreachable) {
			t.Fatalf("safe classification differs: %#v", err)
		}
		if test.kind != "" && !errors.Is(err, test.cause) {
			t.Fatal("standard context identity was discarded")
		}
		var peerError *url.Error
		if errors.As(err, &peerError) {
			t.Fatal("diagnostic retained a sensitive transport error chain")
		}
		for _, rendered := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(rendered, "private-") {
				t.Fatalf("diagnostic exposed underlying peer data: %s", rendered)
			}
		}
	}
}

func TestDiscoverKeepsTheOneMiBResponseReadBound(t *testing.T) {
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{"data_dir": dir, "mock": false})
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, strings.Repeat(" ", (1<<20)-len(body))...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
		_, _ = io.WriteString(w, "private-tail-outside-original-read-limit")
	}))
	defer server.Close()
	record := Record{PID: os.Getpid(), URL: server.URL}
	if err := Publish(dir, record); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(dir, false)
	if err != nil || got != record {
		t.Fatalf("the original bounded prefix behavior changed: record=%+v err=%v", got, err)
	}
}

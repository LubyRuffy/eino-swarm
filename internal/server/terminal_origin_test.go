package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/terminal"
)

func TestSameOriginAllowsBlankAndMatchingHost(t *testing.T) {
	req := &http.Request{Host: "127.0.0.1:8787", RemoteAddr: "127.0.0.1:4321", Header: http.Header{}}
	if !sameOrigin(req) {
		t.Fatal("tests and curl send no Origin")
	}
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	if !sameOrigin(req) {
		t.Fatal("the page that loaded this origin must be allowed")
	}
	req.Header.Set("Origin", "HTTP://127.0.0.1:8787")
	if !sameOrigin(req) {
		t.Fatal("scheme case is not a different site")
	}
}

func TestSameOriginRefusesAnotherHost(t *testing.T) {
	req := &http.Request{Host: "127.0.0.1:8787", RemoteAddr: "127.0.0.1:4321", Header: http.Header{}}
	req.Header.Set("Origin", "https://evil.example")
	if sameOrigin(req) {
		t.Fatal("another tab must not attach to the PTY")
	}
	req.Header.Set("Origin", "http://127.0.0.1:9999")
	if sameOrigin(req) {
		t.Fatal("a different port is a different origin")
	}
	req.Header.Set("Origin", ":not a url")
	if sameOrigin(req) {
		t.Fatal("junk Origin must be refused")
	}
}

func TestSameOriginRefusesARebindHost(t *testing.T) {
	req := &http.Request{Host: "evil.example:8787", RemoteAddr: "127.0.0.1:4321", Header: http.Header{}}
	req.Header.Set("Origin", "http://evil.example:8787")
	if sameOrigin(req) {
		t.Fatal("a DNS-rebind Origin that matches Host is still a foreign page")
	}
}

func TestSameOriginRefusesBlankFromARemotePeer(t *testing.T) {
	req := &http.Request{Host: "127.0.0.1:8787", RemoteAddr: "203.0.113.9:4321", Header: http.Header{}}
	if sameOrigin(req) {
		t.Fatal("a blank Origin from off loopback must not upgrade")
	}
}

func TestTermSizeDefaultsAndClamps(t *testing.T) {
	cols, rows := termSize(url.Values{})
	if cols != terminal.DefaultCols || rows != terminal.DefaultRows {
		t.Fatalf("default %d×%d", cols, rows)
	}
	q := url.Values{}
	q.Set("cols", "12")
	q.Set("rows", "9")
	cols, rows = termSize(q)
	if cols != 12 || rows != 9 {
		t.Fatalf("parsed %d×%d", cols, rows)
	}
	q.Set("cols", "1")
	q.Set("rows", "1")
	cols, rows = termSize(q)
	if cols != 10 || rows != 5 {
		t.Fatalf("clamped %d×%d", cols, rows)
	}
}

func TestApplyTerminalControlResizes(t *testing.T) {
	if err := applyTerminalControl(nil, []byte("not json")); err != nil {
		t.Fatalf("junk: %v", err)
	}
	if err := applyTerminalControl(nil, []byte(`{"type":"input"}`)); err != nil {
		t.Fatalf("unknown type: %v", err)
	}
	// A nil session's Resize is a no-op so a close racing a resize is not a panic.
	if err := applyTerminalControl(nil, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatalf("resize nil: %v", err)
	}
}

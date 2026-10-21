package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

func TestResponsesStreamFailuresNeverLookLikeSuccessfulCompletion(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"failed", `data: {"type":"response.failed","response":{"status":"failed","error":{"message":"upstream failed","type":"server_error"}}}` + "\n\n", "upstream failed"},
		{"incomplete", `data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}` + "\n\n", "max_output_tokens"},
		{"cancelled", `data: {"type":"response.cancelled","response":{}}` + "\n\n", "cancelled"},
		{"failed overrides contradictory status", `data: {"type":"response.failed","response":{"status":"completed"}}` + "\n\n", "failed"},
		{"nested error", `data: {"type":"error","error":{"message":"rejected","code":"denied"}}` + "\n\n", "rejected"},
		{"flat error", `data: {"type":"error","message":"overloaded","code":"server_error"}` + "\n\n", "overloaded"},
		{"wrong delta type", `data: {"type":"response.output_text.delta","delta":5}` + "\n\n", "cannot unmarshal"},
		{"malformed", `data: <html>` + "\n\n", "invalid character"},
		{"truncated", `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n", "unexpected EOF"},
		{"done without terminal", "data: [DONE]\n\n", "unexpected EOF"},
		{"empty body", "", "unexpected EOF"},
		{"arguments before call", `data: {"type":"response.function_call_arguments.delta","delta":"{}"}` + "\n\n", "before its call"},
		{"done arguments before call", `data: {"type":"response.function_call_arguments.done","arguments":"{}"}` + "\n\n", "before its call"},
		{"completed different text", `data: {"type":"response.output_text.delta","delta":"earlier"}` + "\n\n" + `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"different"}]}]}}` + "\n\n", "disagrees"},
		{"completed different reasoning", `data: {"type":"response.reasoning_summary_text.delta","delta":"earlier"}` + "\n\n" + `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","summary":[{"text":"different"}]}]}}` + "\n\n", "disagrees"},
		{"completed failed", `data: {"type":"response.completed","response":{"status":"failed","error":{"message":"bad terminal"}}}` + "\n\n", "bad terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
			sr, err := m.Stream(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = responsesReadAll(t, sr)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestResponsesLiveTextArrivesBeforeCompletionAndCancellationReapsHTTP(t *testing.T) {
	ended := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible before completion\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	first, err := sr.Recv()
	if err != nil || first.Content != "visible before completion" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	cancel()
	_, err = sr.Recv()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cause lost: %v", err)
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream kept its HTTP request alive")
	}
}

func TestResponsesSSEFramesAndFunctionDoneFallback(t *testing.T) {
	frames := ": keepalive\r\n\r\nevent: ignored\r\ndata: {\"type\":\"response.created\"}\r\n\r\n" +
		"event: response.output_item.added\r\ndata: {\"output_index\":0,\r\ndata: \"item\":{\"type\":\"function_call\",\"id\":\"item\",\"call_id\":\"call\",\"name\":\"read\",\"arguments\":\"\"}}\r\n\r\n" +
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{}"}` + "\n\n" +
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call","name":"read","arguments":"{}"}}` + "\n\n" +
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"call","name":"read","arguments":"{}"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, frames) }))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	sr, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := responsesReadAll(t, sr)
	if err != nil {
		t.Fatal(err)
	}
	out, err := schema.ConcatMessages(parts)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].ID != "call" || out.ToolCalls[0].Function.Arguments != "{}" {
		t.Fatalf("calls=%+v", out.ToolCalls)
	}
}

func TestResponsesFunctionIdentityAndFinalArgumentsCannotChange(t *testing.T) {
	for _, tc := range []struct {
		final string
		want  string
	}{
		{`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"other","name":"read","arguments":"{}"}}`, "identity changed"},
		{`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call","name":"read","arguments":"[]"}}`, "disagrees"},
		{`{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"call","name":"read","arguments":"[]"}]}}`, "disagrees"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call","name":"read","arguments":"{}"}}`+"\n\n"+"data: "+tc.final+"\n\n")
		}))
		m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
		sr, err := m.Stream(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = responsesReadAll(t, sr)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestResponsesHTTPTruncationRetainsTransportFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10000")
			w.WriteHeader(status)
			fmt.Fprint(w, "short")
		}))
		m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
		_, err := m.Generate(context.Background(), nil)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("generate err=%v", err)
		}
		sr, err := m.Stream(context.Background(), nil)
		if err == nil {
			_, err = responsesReadAll(t, sr)
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("stream err=%v", err)
		}
		srv.Close()
	}
}

func TestResponsesFailureAndCitationMetadataKeepSafeDisplayBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{"status":"failed","error":{"message":"denied"}}`,
		`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}`,
		`{"status":"queued"}`, `{}`,
	} {
		var response responsesResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			t.Fatal(err)
		}
		if _, err := response.message(); err == nil {
			t.Fatalf("nonterminal response accepted: %s", raw)
		}
	}
	var response responsesResponse
	if err := json.Unmarshal([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"cannot comply"}]}]}`), &response); err != nil {
		t.Fatal(err)
	}
	msg, err := response.message()
	if err != nil || msg.Content != "cannot comply" || msg.ResponseMeta.FinishReason != "stop" {
		t.Fatalf("refusal=%+v err=%v", msg, err)
	}
	got := responsesCitations([]responsesCitation{{Type: "other", URL: "https://ignored.test"}, {Type: "url_citation", URL: "javascript:alert(1)"}, {Type: "url_citation", URL: "https://bad.test/%"}, {Type: "url_citation", URL: "https://example.test/a(b)", Title: "[x]\n"}, {Type: "url_citation", URL: "https://example.test/a(b)", Title: "duplicate"}, {Type: "url_citation", URL: "http://source.test"}})
	if got != "\n\n[\\[x\\] ](https://example.test/a%28b%29) · [http://source.test](http://source.test)" {
		t.Fatalf("unsafe citations=%q", got)
	}
	if got = responsesCitations(nil); got != "" {
		t.Fatal("invented citation")
	}
}

func TestResponsesClosingAnUndrainedReaderStopsFurtherPublication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 64 {
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"chunk\"}\n\n")
		}
	}))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	closedBody := make(chan struct{})
	base := m.client.Transport
	m.client.Transport = responsesRoundTripper(func(req *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(req)
		if err == nil {
			resp.Body = &responsesObservedBody{ReadCloser: resp.Body, closed: closedBody}
		}
		return resp, err
	})
	sr, err := m.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sr.Recv(); err != nil {
		t.Fatal(err)
	}
	sr.Close()
	// Closing a full pipe must release its blocked sender. The transport idle
	// timeout separately bounds a reader closed while waiting on network bytes.
	select {
	case <-closedBody:
	case <-time.After(time.Second):
		t.Fatal("undrained stream retained its response body")
	}
}

type responsesRoundTripper func(*http.Request) (*http.Response, error)

func (f responsesRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type responsesObservedBody struct {
	io.ReadCloser
	closed chan struct{}
}

func (b *responsesObservedBody) Close() error {
	err := b.ReadCloser.Close()
	close(b.closed)
	return err
}

func TestResponsesAllHostedReceiptsStayOutsideLocalToolCalls(t *testing.T) {
	var receipts []BuiltinToolEvent
	ctx := WithBuiltinToolObserver(context.Background(), func(e BuiltinToolEvent) { receipts = append(receipts, e) })
	body := `{"status":"completed","output":[{"type":"web_search_call","id":"web","status":"completed"},{"type":"web_search_call","id":"web","status":"completed"},{"type":"file_search_call","id":"file","status":"completed","queries":["knowledge"],"results":[{"file_id":"f_fixture"}]},{"type":"code_interpreter_call","id":"code","status":"completed","code":"print(2)","container_id":"cn_fixture","outputs":[{"type":"logs","logs":"2"}]},{"type":"web_search_call","status":"completed"},{"type":"file_search_call","id":"pending"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	out, err := m.Generate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 3 || len(out.ToolCalls) != 0 {
		t.Fatalf("hosted receipts=%+v local=%+v", receipts, out.ToolCalls)
	}
	if !strings.Contains(string(receipts[1].Details), `"queries":["knowledge"]`) || !strings.Contains(string(receipts[2].Details), `"container_id":"cn_fixture"`) || !strings.Contains(string(receipts[2].Details), `"logs":"2"`) {
		t.Fatalf("provider execution details missing from receipts: %+v", receipts)
	}
}

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// These routes are test-only. They use separate fixed fixture authority and
// counters, never a production provider or an injected phone session.
func (f *fixture) personalModel(w http.ResponseWriter, r *http.Request) {
	if f.flow != nil && r.Header.Get("Authorization") == "Bearer "+f.flow.Candidate {
		f.counts["personal_mobile_token_leak"]++
	}
	if r.Header.Get("Authorization") != "Bearer native-personal-key" {
		http.Error(w, "personal fixture key required", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Cookie") != "" {
		f.counts["personal_cookie_leak"]++
	}
	switch r.URL.Path {
	case "/personal/v1/models":
		f.counts["personal_models"]++
		data(w, []map[string]string{{"id": "native-personal-model"}})
	case "/personal/v1/chat/completions":
		f.model(w, r, "personal")
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) model(w http.ResponseWriter, r *http.Request, source string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	var input struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	model := "native-fixture-model"
	if source == "personal" {
		model = "native-personal-model"
	}
	leak := f.flow != nil && strings.Contains(string(body), f.flow.Candidate)
	if err != nil || json.Unmarshal(body, &input) != nil || input.Model != model || !input.Stream || len(input.Messages) == 0 || leak {
		f.counts["invalid_model_contract"]++
		http.Error(w, "invalid fixture model invocation", http.StatusBadRequest)
		return
	}
	f.counts["model_calls"]++
	f.counts[source+"_model_calls"]++
	if f.modelMode == "redirect" {
		f.counts["model_redirect_sent"]++
		http.Redirect(w, r, f.anonymous+"/fixture/forbidden-target", http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	if f.modelMode == "stall" && !f.waitCancellation(w, r, "model") {
		return
	}
	if f.modelMode == "tools" {
		if len(input.Tools) == 0 {
			f.modelText(w, "Native ordinary answer")
			return
		}
		if len(input.Tools) != 2 || input.Tools[0].Type != "function" || input.Tools[1].Type != "function" || input.Tools[0].Function.Name != "web_search" || input.Tools[1].Function.Name != "web_fetch" {
			f.counts["invalid_model_contract"]++
			http.Error(w, "invalid native function tools", http.StatusBadRequest)
			return
		}
		outputs := 0
		for _, message := range input.Messages {
			if message.Role == "user" {
				outputs = 0
			} else if message.Role == "tool" {
				outputs++
			}
		}
		if outputs >= 2 {
			f.modelText(w, "Native cloud evidence answer")
			return
		}
		name, args := "web_search", `{"query":"current information"}`
		if outputs == 1 {
			name, args = "web_fetch", `{"url":"https://public.invalid/page","render":true}`
		}
		call := map[string]any{"index": 0, "id": "native-" + name, "type": "function", "function": map[string]any{"name": name, "arguments": args}}
		sse(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}, "finish_reason": "tool_calls"}}})
		return
	}
	answer := "Native account answer"
	if source == "personal" {
		answer = "Native ordinary answer"
	}
	f.modelText(w, answer)
}

func (f *fixture) modelText(w http.ResponseWriter, answer string) {
	f.counts["model_successes"]++
	sse(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}, "finish_reason": "stop"}}})
}

func sse(w http.ResponseWriter, value any) {
	raw, _ := json.Marshal(value)
	_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: [DONE]\n\n")
}

// The caller owns f.mu; release it while waiting so the fixture state remains
// observable during a real native network request and cancellation.
func (f *fixture) waitCancellation(w http.ResponseWriter, r *http.Request, kind string) bool {
	f.counts[kind+"_stall_started"]++
	w.WriteHeader(http.StatusOK)
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	f.mu.Unlock()
	select {
	case <-r.Context().Done():
		f.mu.Lock()
		f.counts[kind+"_cancelled"]++
		return false
	case <-time.After(30 * time.Second):
		f.mu.Lock()
		f.counts[kind+"_completed"]++
		return true
	}
}

func (f *fixture) webTool(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Query   string   `json:"query"`
		URL     string   `json:"url"`
		Formats []string `json:"formats"`
		Actions []struct {
			Type         string `json:"type"`
			Milliseconds int    `json:"milliseconds"`
		} `json:"actions"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input) != nil {
		http.Error(w, "invalid native Web request", http.StatusBadRequest)
		return
	}
	search := strings.HasSuffix(r.URL.Path, "/search")
	if search && input.Query != "current information" || !search && (input.URL != "https://public.invalid/page" || len(input.Formats) != 1 || input.Formats[0] != "markdown" || len(input.Actions) != 1 || input.Actions[0].Type != "wait" || input.Actions[0].Milliseconds != 1) {
		f.counts["invalid_web_contract"]++
		http.Error(w, "invalid native Web contract", http.StatusBadRequest)
		return
	}
	if search {
		f.counts["web_search"]++
	} else {
		f.counts["web_fetch"]++
	}
	if f.webMode == "denied" {
		f.counts["web_denied"]++
		http.Error(w, "native fixture Web permission denied", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.webMode == "stall" && !f.waitCancellation(w, r, "web") {
		return
	}
	value := any(map[string]any{"markdown": "Native fetched page evidence"})
	if search {
		value = map[string]any{"results": []any{map[string]any{"title": "Native search evidence", "url": "https://public.invalid/page", "snippet": "Native search summary"}}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": value})
}

func (f *fixture) observeWebRouting(value map[string]any) {
	if f.engine == nil {
		return
	}
	value["pc_web_status"] = tools.DescribeWebExecution(f.engine.Config())
	if thread, err := f.engine.Store().GetThread(f.pcThreadID); err == nil {
		value["pc_thread_web_execution"] = thread.WebExecution
	}
	if id, err := f.engine.Store().LatestTurnID(f.pcThreadID); err == nil {
		if turn, err := f.engine.Store().GetTurn(id); err == nil {
			value["pc_turn_web_execution"] = turn.WebExecution
		}
	}
}

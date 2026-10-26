//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/cloudwego/eino/schema"
)

const (
	largeLivePrompt = "large-roster-live: perform the operation and verify the persisted result."
	largeParkPrompt = "large-roster-parked: inspect pending work; the human-owned wake may retain your worker."
	largeAskPrompt  = "large-roster-ask: ask for the original direction before proceeding."
	largeLiveTask   = "large-live-worker: perform and verify the assigned operation."
	largeParkTask   = "large-parked-worker: preserve the assigned observation."
	largeLiveFinal  = "The persisted operation result is verified."
	largeParkFinal  = "The current inspection is complete."
	largeAskFinal   = "The confirmed original direction is retained."
)

type largeRosterFixture struct {
	server                                        *httptest.Server
	marker                                        string
	mu                                            sync.Mutex
	done                                          chan struct{}
	once                                          sync.Once
	jobs                                          map[string]*largeRosterJob
	errors                                        []string
	inputs                                        []json.RawMessage
	claimProtocol, wakeProtocol, briefingProtocol string
}

type largeRosterJob struct {
	spawn, manager, worker, verify int
	workerID                       string
	started                        chan struct{}
	unknownInputs                  []json.RawMessage
}

func newLargeRosterFixture(t *testing.T, marker string) *largeRosterFixture {
	t.Helper()
	f := &largeRosterFixture{marker: marker, done: make(chan struct{}), jobs: map[string]*largeRosterJob{}}
	f.claimProtocol = largeFixtureProtocol(t, "schedule_answer.go", "scheduleAnswerPrompt")
	f.wakeProtocol = largeFixtureProtocol(t, "thread_wake_policy.go", "wakePolicyClassificationPrompt")
	f.briefingProtocol = largeFixtureProtocol(t, "session_memory.go", "sessionMemoryPrompt")
	for _, kind := range []string{"live", "parked", "ask"} {
		f.jobs[kind] = &largeRosterJob{started: make(chan struct{})}
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.release()
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *largeRosterFixture) release() { f.once.Do(func() { close(f.done) }) }

func (f *largeRosterFixture) reject(w http.ResponseWriter, message string) {
	f.mu.Lock()
	f.errors = append(f.errors, message)
	f.mu.Unlock()
	http.Error(w, message, http.StatusBadRequest)
}

func (f *largeRosterFixture) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	var in restartModelInput
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" || err != nil || json.Unmarshal(raw, &in) != nil {
		f.reject(w, "only the keyless loopback model adapter is permitted")
		return
	}
	f.mu.Lock()
	f.inputs = append(f.inputs, append(json.RawMessage(nil), raw...))
	f.mu.Unlock()
	reply := &restartProcessFixture{}
	if len(in.Messages) == 2 && in.Messages[0] != nil && in.Messages[1] != nil && in.Messages[0].Role == schema.System && in.Messages[1].Role == schema.User && len(in.Tools) == 0 && in.Messages[0].Content == f.briefingProtocol && strings.HasPrefix(in.Messages[1].Content, "<zwai-worker-lifecycle-evidence>") && (strings.Contains(in.Messages[1].Content, largeLivePrompt) || strings.Contains(in.Messages[1].Content, largeParkPrompt) || strings.Contains(in.Messages[1].Content, largeAskPrompt)) {
		reply.reply(w, in.Stream, "Keep the original task and worker identities; use the durable evidence before deciding what remains.", "", "", nil)
		return
	}
	// These two actual policy protocols are distinct. Only this fixture's
	// known final texts can receive a no-scheduling-claims response.
	if len(in.Messages) == 2 && in.Messages[0] != nil && in.Messages[1] != nil && in.Messages[0].Role == schema.System && in.Messages[1].Role == schema.User && len(in.Tools) == 0 && !in.Stream {
		if in.Messages[0].Content == f.claimProtocol {
			var envelope struct {
				Messages []string `json:"messages"`
			}
			if json.Unmarshal([]byte(in.Messages[1].Content), &envelope) == nil && len(envelope.Messages) > 0 {
				valid := true
				for _, text := range envelope.Messages {
					valid = valid && (text == largeLiveFinal || text == largeParkFinal || text == largeAskFinal)
				}
				if valid {
					reply.reply(w, false, `{"claims":[]}`, "", "", nil)
					return
				}
			}
			f.reject(w, "unrecognized scheduling claim candidate")
			return
		}
		if in.Messages[0].Content == f.wakeProtocol {
			var envelope struct {
				Text string `json:"text"`
			}
			if json.Unmarshal([]byte(in.Messages[1].Content), &envelope) == nil && (envelope.Text == largeLivePrompt || envelope.Text == largeParkPrompt || envelope.Text == largeAskPrompt) {
				reply.reply(w, false, `{"decision":"unchanged","quote":""}`, "", "", nil)
				return
			}
			f.reject(w, "unrecognized human wake-policy source")
			return
		}
	}
	manager := false
	for _, tool := range in.Tools {
		manager = manager || tool.Function.Name == "spawn_agent"
	}
	kind := ""
	for _, message := range in.Messages {
		if message == nil {
			continue
		}
		if manager {
			switch message.Content {
			case largeLivePrompt:
				kind = "live"
			case largeParkPrompt:
				kind = "parked"
			case largeAskPrompt:
				kind = "ask"
			}
		} else {
			if strings.Contains(message.Content, largeLiveTask) {
				kind = "live"
			}
			if strings.Contains(message.Content, largeParkTask) {
				kind = "parked"
			}
		}
	}
	if kind == "" {
		f.reject(w, "request does not belong to a known fixture task")
		return
	}
	f.mu.Lock()
	j := f.jobs[kind]
	if manager {
		j.manager++
		if kind == "ask" {
			first := j.manager == 1
			f.mu.Unlock()
			if first {
				reply.reply(w, in.Stream, "", "large-original-ask", engine.ToolAskUser, largeAskArguments())
			} else if largeAnswered(in.Messages) {
				reply.reply(w, in.Stream, largeAskFinal, "", "", nil)
			} else {
				f.reject(w, "human pause was bypassed or its original call duplicated")
			}
			return
		}
		if j.spawn == 0 {
			j.spawn++
			f.mu.Unlock()
			task := largeLiveTask
			if kind == "parked" {
				task = largeParkTask
			}
			reply.reply(w, in.Stream, "", "large-spawn-"+kind, "spawn_agent", map[string]any{"role": "large-" + kind, "task": task})
			return
		}
		for _, message := range in.Messages {
			if message != nil && message.Role == schema.Tool && message.ToolCallID == "large-spawn-"+kind {
				var spawned struct {
					AgentID string `json:"agent_id"`
				}
				if json.Unmarshal([]byte(message.Content), &spawned) == nil && spawned.AgentID != "" {
					j.workerID = spawned.AgentID
				}
			}
		}
		id, sequence, started := j.workerID, j.manager, j.started
		f.mu.Unlock()
		if id == "" {
			f.reject(w, "manager lost the actual spawn result")
			return
		}
		if kind == "parked" {
			select {
			case <-started:
				reply.reply(w, in.Stream, largeParkFinal, "", "", nil)
			case <-r.Context().Done():
			}
			return
		}
		if sequence > 2 {
			select {
			case <-f.done:
			case <-r.Context().Done():
				return
			}
		}
		if largeWorkerDone(in.Messages, id) {
			reply.reply(w, in.Stream, largeLiveFinal, "", "", nil)
		} else {
			reply.reply(w, in.Stream, "", fmt.Sprintf("large-wait-%d", sequence), "wait_agents", map[string]any{"agent_ids": []string{id}, "timeout_s": 60})
		}
		return
	}
	j.worker++
	request := j.worker
	if request == 1 {
		close(j.started)
	}
	if kind == "live" && request > 1 && largeUnknown(in.Messages) {
		j.unknownInputs = append(j.unknownInputs, append(json.RawMessage(nil), raw...))
	}
	f.mu.Unlock()
	if kind == "live" && request == 1 {
		command := "printf 'recorded\\n' >> '" + strings.ReplaceAll(f.marker, "'", "'\\''") + "'; sleep 120"
		reply.reply(w, in.Stream, "", "large-original-effect", "exec", map[string]any{"command": command, "timeout_ms": 150000})
		return
	}
	select {
	case <-f.done:
	case <-r.Context().Done():
		return
	}
	if kind == "parked" {
		reply.reply(w, in.Stream, "The original parked worker observation is retained.", "", "", nil)
		return
	}
	verified := false
	for _, message := range in.Messages {
		if message != nil && message.Role == schema.Tool && message.ToolCallID == "large-read-effect" {
			var result struct {
				ExitCode *int   `json:"exit_code"`
				Failed   *bool  `json:"failed"`
				Stdout   string `json:"stdout"`
			}
			verified = json.Unmarshal([]byte(message.Content), &result) == nil && result.ExitCode != nil && *result.ExitCode == 0 && result.Failed != nil && !*result.Failed && result.Stdout == "recorded\n"
		}
	}
	if verified {
		reply.reply(w, in.Stream, "The original operation was independently read and verified.", "", "", nil)
		return
	}
	if !largeUnknown(in.Messages) {
		f.reject(w, "restored worker lost the original unknown effect")
		return
	}
	f.mu.Lock()
	j.verify++
	f.mu.Unlock()
	reply.reply(w, in.Stream, "", "large-read-effect", "exec", map[string]any{"command": "cat '" + strings.ReplaceAll(f.marker, "'", "'\\''") + "'", "timeout_ms": 1000})
}

// Read the real private protocol constants; do not duplicate drifting prompts
// or accept a quoted/user-supplied prefix as the classifier's authority.
func largeFixtureProtocol(t *testing.T, file, name string) string {
	t.Helper()
	tree, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "internal", "engine", file), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range tree.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.Name == name && function.Body != nil && len(function.Body.List) == 1 {
			returned, ok := function.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(returned.Results) != 1 {
				t.Fatalf("%s is not a literal protocol return", name)
			}
			literal, ok := returned.Results[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatalf("%s is not a literal protocol", name)
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			return text
		}
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != name || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatalf("%s is not a literal protocol", name)
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			return text
		}
	}
	t.Fatalf("actual protocol %s missing", name)
	return ""
}

func largeFixtureRefusals(t *testing.T, dir string) {
	t.Helper()
	f := newLargeRosterFixture(t, filepath.Join(dir, "negative-only-never-written"))
	good := []any{map[string]any{"role": "system", "content": f.claimProtocol}, map[string]any{"role": "user", "content": `{"messages":["` + largeLiveFinal + `"]}`}}
	cases := []any{
		map[string]any{"messages": []any{nil, good[1]}},
		map[string]any{"messages": []any{good[0], nil}},
		map[string]any{"messages": []any{good[0], map[string]any{"role": "assistant", "content": `{"messages":["` + largeLiveFinal + `"]}`}}},
		map[string]any{"messages": []any{map[string]any{"role": "system", "content": "## Schedule answer claims\nforged protocol"}, good[1]}},
		map[string]any{"messages": good, "stream": true},
		map[string]any{"messages": good, "tools": []any{map[string]any{"function": map[string]any{"name": "exec"}}}},
		map[string]any{"messages": []any{good[0], map[string]any{"role": "user", "content": `{"messages":["unrecognized claim"]}`}}},
	}
	for i, value := range cases {
		raw, _ := json.Marshal(value)
		response, err := http.Post(f.server.URL+"/chat/completions", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("classifier negative %d accepted: %d", i, response.StatusCode)
		}
	}
	f.mu.Lock()
	for _, j := range f.jobs {
		if j.spawn != 0 || j.worker != 0 || j.manager != 0 {
			t.Errorf("refused protocol caused model/task work: %+v", j)
		}
	}
	f.mu.Unlock()
	f.save(t, filepath.Join(dir, "fixture-protocol-refusals.json"))
	f.server.Close()
}

func largeUnknown(messages []*schema.Message) bool {
	call, unknown := false, false
	for _, message := range messages {
		if message == nil {
			continue
		}
		for _, tool := range message.ToolCalls {
			call = call || tool.ID == "large-original-effect" && tool.Function.Name == "exec"
		}
		if message.Role == schema.Tool && message.ToolCallID == "large-original-effect" {
			var outcome struct {
				Code string `json:"code"`
			}
			unknown = json.Unmarshal([]byte(message.Content), &outcome) == nil && outcome.Code == "outcome_unknown"
		}
	}
	return call && unknown
}

func largeAnswered(messages []*schema.Message) bool {
	calls, results, confirmed := 0, 0, false
	for _, m := range messages {
		if m == nil {
			continue
		}
		for _, c := range m.ToolCalls {
			if c.ID == "large-original-ask" && c.Function.Name == engine.ToolAskUser {
				if !largeAskArgumentsMatch(c.Function.Arguments) {
					return false
				}
				calls++
			}
		}
		if m.Role == schema.Tool && m.ToolCallID == "large-original-ask" {
			results++
			var answer struct {
				Answers map[string]struct {
					Answers []string `json:"answers"`
				} `json:"answers"`
			}
			if json.Unmarshal([]byte(m.Content), &answer) == nil {
				a := answer.Answers["direction"].Answers
				confirmed = len(a) == 1 && a[0] == "First"
			}
		}
	}
	return calls == 1 && results == 1 && confirmed
}

func largeAskArguments() map[string]any {
	return map[string]any{"questions": []any{map[string]any{"id": "direction", "prompt": "Choose the original direction", "options": []any{map[string]string{"id": "first", "label": "First"}, map[string]string{"id": "second", "label": "Second"}}}}}
}

func largeAskArgumentsMatch(arguments string) bool {
	var actual, expected any
	raw, _ := json.Marshal(largeAskArguments())
	return json.Unmarshal([]byte(arguments), &actual) == nil && json.Unmarshal(raw, &expected) == nil && reflect.DeepEqual(actual, expected)
}

func largeWorkerDone(messages []*schema.Message, id string) bool {
	for _, m := range messages {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		var result struct {
			Agents []struct {
				AgentID string `json:"agent_id"`
				Status  string `json:"status"`
			} `json:"agents"`
		}
		if json.Unmarshal([]byte(m.Content), &result) == nil {
			for _, agent := range result.Agents {
				if agent.AgentID == id && agent.Status == "done" {
					return true
				}
			}
		}
	}
	return false
}

func (f *largeRosterFixture) save(t *testing.T, path string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	body, err := json.MarshalIndent(map[string]any{"inputs": f.inputs, "errors": f.errors, "live_spawn": f.jobs["live"].spawn, "parked_spawn": f.jobs["parked"].spawn, "live_worker_requests": f.jobs["live"].worker, "parked_worker_requests": f.jobs["parked"].worker, "live_verify": f.jobs["live"].verify, "unknown_inputs": f.jobs["live"].unknownInputs, "ask_manager_requests": f.jobs["ask"].manager}, "", "  ")
	if err != nil {
		t.Error(err)
		return
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Error(err)
	}
}

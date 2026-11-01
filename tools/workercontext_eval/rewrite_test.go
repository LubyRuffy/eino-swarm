package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestActualRegistryDuplicateWorkerEntriesCannotSupplyAnAmbiguousReference(t *testing.T) {
	reg := swarm.NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: "w", Role: "reader", Generation: 2, Result: "full result"}); err != nil {
		t.Fatal(err)
	}
	var wait tool.InvokableTool
	for _, base := range reg.Tools() {
		info, err := base.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "wait_agents" {
			wait = base.(tool.InvokableTool)
		}
	}
	duplicate, err := wait.InvokableRun(context.Background(), `{"agent_ids":["w","w"]}`)
	if err != nil {
		t.Fatal(err)
	}
	single, err := wait.InvokableRun(context.Background(), `{"agent_ids":["w"]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]*schema.Message{
		append(waitPair("first", duplicate), waitPair("later", single)...),
		append(waitPair("first", single), waitPair("later", duplicate)...),
	} {
		before, _ := json.Marshal(input)
		out, changes := referenceDuplicates(input)
		after, _ := json.Marshal(out)
		if len(changes) != 0 || !reflect.DeepEqual(before, after) {
			t.Fatal("ambiguous native worker entries were referenced")
		}
	}
}

func waitPair(id, body string) []*schema.Message {
	return []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: id, Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{}`}}}),
		schema.ToolMessage(body, id, schema.WithToolName("wait_agents")),
	}
}

func report(id string, generation int, body string) string {
	b, _ := json.Marshal(map[string]any{"agents": []any{map[string]any{
		"agent_id": id, "generation": generation, "status": "done", "result": body,
		"undelivered": []string{"a condition must remain negative"}}}, "timed_out": false})
	return string(b)
}

func TestOnlyLaterExactSameGenerationResultsBecomeReferences(t *testing.T) {
	body := strings.Repeat("unchanged result\n", 5000) + "amount=-73; valid only if disabled=false"
	first := report("worker-1", 4, body)
	in := append(waitPair("first", first), waitPair("second", first)...)
	before, _ := json.Marshal(in)
	out, changes := referenceDuplicates(in)
	if len(changes) != 1 || out[1].Content != first || strings.Contains(out[3].Content, body) {
		t.Fatalf("wrong rewritten messages: changes=%+v", changes)
	}
	var doc struct {
		Agents []map[string]json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out[3].Content), &doc); err != nil {
		t.Fatal(err)
	}
	var ref resultReference
	if err := json.Unmarshal(doc.Agents[0]["result_ref"], &ref); err != nil {
		t.Fatal(err)
	}
	if ref.ToolCallID != "first" || ref.AgentID != "worker-1" || ref.Generation != 4 || ref.SHA256 != resultHash(body, "") {
		t.Fatalf("reference lost exact identity: %+v", ref)
	}
	if _, exists := doc.Agents[0]["result"]; exists {
		t.Fatal("duplicate result remained")
	}
	if string(doc.Agents[0]["undelivered"]) != `["a condition must remain negative"]` {
		t.Fatal("undelivered changed")
	}
	after, _ := json.Marshal(in)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rewriter mutated durable input")
	}
	if !strings.Contains(out[1].Content, "amount=-73; valid only if disabled=false") {
		t.Fatal("full original lost")
	}
}

func TestNoCrossRequestCacheOrUnprovenResults(t *testing.T) {
	full := report("w", 2, "body")
	for _, test := range []struct{ name, second string }{
		{"new-generation", report("w", 3, "body")},
		{"new-result", report("w", 2, "body changed")},
		{"different-worker", report("other", 2, "body")},
		{"no-generation", `{"agents":[{"agent_id":"w","status":"done","result":"body"}]}`},
		{"failure", `{"agents":[{"agent_id":"w","generation":2,"status":"failed","result":"body","error":"failed"}]}`},
		{"error", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","error":"incomplete"}]}`},
		{"unknown", `{"agents":[{"agent_id":"w","generation":2,"status":"unknown","result":"body"}]}`},
		{"invalid", `{"agents":`},
		{"bad-generation", `{"agents":[{"agent_id":"w","generation":2.5,"status":"done","result":"body"}]}`},
		{"bad-error", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","error":false}]}`},
		{"null-error", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","error":null}]}`},
		{"bad-role", `{"agents":[{"agent_id":"w","generation":2,"role":3,"status":"done","result":"body"}]}`},
		{"existing-ref", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","result_ref":{}}]}`},
		{"duplicate-key", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","result":"body"}]}`},
		{"duplicate-nested-key", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body","extra":{"a":1,"a":2}}]}`},
		{"truncated-array", `{"agents":[{"agent_id":"w","generation":2,"status":"done","result":"body"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			in := append(waitPair("first", full), waitPair("second", test.second)...)
			out, changes := referenceDuplicates(in)
			if len(changes) != 0 || out[3].Content != test.second {
				t.Fatalf("unproven body rewritten: %+v", changes)
			}
		})
	}
	_, _ = referenceDuplicates(append(waitPair("a", full), waitPair("b", full)...))
	current := waitPair("only", full)
	out, changes := referenceDuplicates(current)
	if len(changes) != 0 || out[1].Content != full {
		t.Fatal("earlier body from a previous request was trusted")
	}
	truncated := append(waitPair("old", full[:len(full)/2]), waitPair("new", full)...)
	if _, changes := referenceDuplicates(truncated); len(changes) != 0 {
		t.Fatal("truncated prior body supplied a reference")
	}
	nonwait := append(waitPair("a", full), waitPair("b", full)...)
	nonwait[2].ToolCalls[0].Function.Name = "read"
	if _, changes := referenceDuplicates(nonwait); len(changes) != 0 {
		t.Fatal("a non-wait result was changed")
	}
}

func TestAmbiguousCallsAndMissingPairsCannotSupplyFullBodyEvidence(t *testing.T) {
	full := report("w", 2, "body")
	for _, mutate := range []func([]*schema.Message) []*schema.Message{
		func(in []*schema.Message) []*schema.Message { return append(in, in[0]) },
		func(in []*schema.Message) []*schema.Message { return append(in, in[1]) },
		func(in []*schema.Message) []*schema.Message { in[0] = nil; return in },
		func(in []*schema.Message) []*schema.Message { in[1].ToolName = "read"; return in },
		func(in []*schema.Message) []*schema.Message { in[0], in[1] = in[1], in[0]; return in },
		func(in []*schema.Message) []*schema.Message { in[1].Content = `{"agents":[3]}`; return in },
		func(in []*schema.Message) []*schema.Message { in[1].Content = `null`; return in },
	} {
		in := mutate(append(waitPair("a", full), waitPair("b", full)...))
		before, _ := json.Marshal(in)
		out, changes := referenceDuplicates(in)
		after, _ := json.Marshal(out)
		if len(changes) != 0 || !reflect.DeepEqual(before, after) {
			t.Fatal("unproven source was referenced")
		}
	}
	for _, body := range []string{`{"a":[1,true,null,"x",{}]}`, `{}`, `[]`, `"primitive"`} {
		if !uniqueJSON(body) {
			t.Fatalf("valid JSON refused: %s", body)
		}
	}
	for _, body := range []string{`{"a":[]`, `[{`, `{"x":1} {"x":2}`, `{"a":1,"a":2}`} {
		if uniqueJSON(body) {
			t.Fatalf("malformed/ambiguous JSON accepted: %s", body)
		}
	}
}

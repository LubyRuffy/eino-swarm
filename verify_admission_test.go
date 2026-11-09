package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func verificationRegistry(t *testing.T, admit func(context.Context, string, string, string) error) *Registry {
	t.Helper()
	reg := NewRegistry()
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("done", nil) },
		}}
	}
	reg.VerifyAdmission = admit
	reg.ConfigureWorkers(reg.WorkerConfiguration())
	t.Cleanup(reg.Close)
	return reg
}

func spawnOut(t *testing.T, reg *Registry, args string) string {
	t.Helper()
	out, err := invokable(t, reg.Tools()[0]).InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOrdinarySpawnIgnoresVerificationFields(t *testing.T) {
	reg := verificationRegistry(t, nil)
	info, err := reg.Tools()[0].Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"intent"`) || strings.Contains(string(encoded), `"target"`) {
		t.Fatalf("ordinary spawn advertised verification fields: %s", encoded)
	}
	out := spawnOut(t, reg, `{"role":"implementer","task":"do the assigned work","intent":"verify","target":"nw_ignored"}`)
	if !strings.Contains(out, "agent_id") || strings.Contains(out, "error") {
		t.Fatalf("ordinary mode treated verification fields as a gate: %s", out)
	}
}

func TestNuwaSpawnRefusesVerificationBeforeAWorkerExists(t *testing.T) {
	var calls int
	reg := verificationRegistry(t, func(context.Context, string, string, string) error {
		calls++
		return nil
	})
	info, err := reg.Tools()[0].Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Desc, "one verify and one fix") {
		t.Fatal("Nuwa spawn description does not say the host enforces the allowance")
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"intent"`) || !strings.Contains(string(encoded), `"target"`) {
		t.Fatalf("Nuwa spawn hid verification fields: %s", encoded)
	}
	assertCtlRefuse(t, spawnOut(t, reg, `{"role":"verifier","task":"check","intent":"audit","target":"nw_1"}`), nil, "intent must be work, verify, or fix")
	assertCtlRefuse(t, spawnOut(t, reg, `{"role":"verifier","task":"check","intent":"verify"}`), nil, "require target")
	assertCtlRefuse(t, spawnOut(t, reg, `{"role":"verifier","task":"check","intent":"verify","target":"nw_1"}`), nil, "real tool call identity")
	if calls != 0 || len(reg.PendingAgents()) != 0 {
		t.Fatalf("refused verification reached the host or started a worker: calls=%d pending=%d", calls, len(reg.PendingAgents()))
	}
	out := spawnOut(t, reg, `{"role":"implementer","task":"do the assigned work"}`)
	if !strings.Contains(out, "agent_id") || calls != 0 {
		t.Fatalf("ordinary work was charged or refused: %s calls=%d", out, calls)
	}
}

func TestNuwaSpawnAdmissionSeesTheRealToolCallBeforeStartingAWorker(t *testing.T) {
	var calls int
	reg := verificationRegistry(t, func(_ context.Context, intent, target, callID string) error {
		calls++
		if intent != "verify" || target != "nw_record" || callID != "verify-call" {
			t.Errorf("admission saw intent=%q target=%q call=%q", intent, target, callID)
		}
		if calls > 1 {
			return errors.New("this turn already used its one verify")
		}
		return nil
	})
	node, err := compose.NewToolNode(t.Context(), &compose.ToolsNodeConfig{Tools: reg.Tools()})
	if err != nil {
		t.Fatal(err)
	}
	call := schema.AssistantMessage("", []schema.ToolCall{{
		ID: "verify-call", Type: "function",
		Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"checker","task":"check the recorded deliverable","intent":"verify","target":"nw_record"}`},
	}})
	first, err := node.Invoke(t.Context(), call)
	if err != nil || len(first) != 1 || !strings.Contains(first[0].Content, "agent_id") || calls != 1 {
		t.Fatalf("first admitted check: %+v err=%v calls=%d", first, err, calls)
	}
	second, err := node.Invoke(t.Context(), call)
	if err != nil || len(second) != 1 || strings.Contains(second[0].Content, "agent_id") || !strings.Contains(second[0].Content, "one verify") {
		t.Fatalf("second check started a worker: %+v err=%v calls=%d", second, err, calls)
	}
	if _, finished := reg.Stats(); finished+len(reg.PendingAgents()) != 1 {
		running, done := reg.Stats()
		t.Fatalf("admission changed the worker count: running=%d finished=%d pending=%d", running, done, len(reg.PendingAgents()))
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err, refused := reg.refuseVerification(cancelled, spawnArgs{Role: "checker", Task: "check", Intent: "verify", Target: "nw_record"})
	if !refused || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled verification became a successful refusal: refused=%v err=%v", refused, err)
	}
}

package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type workerMiddlewareRequest struct {
	method, model string
	messages      []*schema.Message
}

type workerMiddlewareProbe struct {
	name, tool, arguments, callID string
	requests                      chan workerMiddlewareRequest
	release                       <-chan struct{}
	rounds                        int
}

func (m *workerMiddlewareProbe) reply(ctx context.Context, method string, messages []*schema.Message) (*schema.Message, error) {
	select {
	case m.requests <- workerMiddlewareRequest{method: method, model: m.name, messages: cloneMessages(messages)}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	first := m.rounds == 0
	m.rounds++
	if first && m.release != nil {
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if first && m.tool != "" {
		return schema.AssistantMessage("", []schema.ToolCall{rawCall(m.callID, m.tool, m.arguments)}), nil
	}
	return schema.AssistantMessage("complete:"+m.name, nil), nil
}

func (m *workerMiddlewareProbe) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return m.reply(ctx, "generate", messages)
}

func (m *workerMiddlewareProbe) Stream(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.reply(ctx, "stream", messages)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type workerMiddlewareInvocation struct {
	factory  string
	identity AgentIdentity
}

type workerInputMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	marker string
}

func (m *workerInputMiddleware) WrapModel(_ context.Context, inner model.BaseModel[*schema.Message], _ *adk.ModelContext) (model.BaseModel[*schema.Message], error) {
	return &workerInputModel{inner: inner, marker: m.marker}, nil
}

type workerInputModel struct {
	inner  model.BaseModel[*schema.Message]
	marker string
}

func (m *workerInputModel) input(messages []*schema.Message) []*schema.Message {
	return append(append([]*schema.Message(nil), messages...), schema.SystemMessage(m.marker))
}

func (m *workerInputModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, m.input(messages), opts...)
}

func (m *workerInputModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, m.input(messages), opts...)
}

func workerMiddlewareFactory(t *testing.T, name string, calls chan workerMiddlewareInvocation) func(AgentIdentity) []adk.ChatModelAgentMiddleware {
	return func(identity AgentIdentity) []adk.ChatModelAgentMiddleware {
		select {
		case calls <- workerMiddlewareInvocation{factory: name, identity: identity}:
		default:
			t.Errorf("factory %s exceeded its invocation budget for %+v", name, identity)
		}
		return []adk.ChatModelAgentMiddleware{&workerInputMiddleware{marker: workerMiddlewareMarker(name, identity)}}
	}
}

func workerMiddlewareMarker(name string, identity AgentIdentity) string {
	return fmt.Sprintf("host-context:%s:%s:%d", name, identity.AgentID, identity.Generation)
}

func workerMiddlewareBuilder(name string, requests chan workerMiddlewareRequest, release <-chan struct{}) ModelBuilder {
	var sequence atomic.Uint64
	return func(string, string) model.BaseChatModel {
		return &workerMiddlewareProbe{name: name, tool: name + "_surface", arguments: `{}`, callID: fmt.Sprintf("probe-%s-%d", name, sequence.Add(1)), requests: requests, release: release}
	}
}

func workerMiddlewareControl(t *testing.T, reg *Registry, name, arguments string) *Handle {
	t.Helper()
	var out string
	var err error
	if name == "spawn_agent" {
		out, err = reg.spawn(t.Context(), arguments)
	} else {
		out, err = reg.resume(t.Context(), arguments)
	}
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		AgentID string `json:"agent_id"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.Error != "" || receipt.AgentID == "" {
		t.Fatalf("%s failed: %s (%v)", name, out, err)
	}
	h, ok := reg.get(receipt.AgentID)
	if !ok {
		t.Fatalf("%s did not register %s", name, receipt.AgentID)
	}
	return h
}

func workerMiddlewareRequestAt(t *testing.T, requests chan workerMiddlewareRequest) workerMiddlewareRequest {
	t.Helper()
	return workerMiddlewareValueAt(t, requests, "actual model request")
}

func workerMiddlewareValueAt[T any](t *testing.T, values <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not arrive", description)
		var zero T
		return zero
	}
}

func assertWorkerMiddlewareRequests(t *testing.T, requests []workerMiddlewareRequest, method, name string, identity AgentIdentity, injected bool) {
	t.Helper()
	for _, request := range requests {
		if request.method != method || request.model != name {
			t.Fatalf("worker model path changed: method=%s model=%s", request.method, request.model)
		}
		markers := 0
		for _, message := range request.messages {
			if strings.HasPrefix(message.Content, "host-context:") {
				markers++
				if !injected || message.Content != workerMiddlewareMarker(name, identity) {
					t.Fatalf("wrong host context at actual model: %q", message.Content)
				}
			}
		}
		if injected && markers != 1 || !injected && markers != 0 {
			t.Fatalf("actual model received %d host contexts, injected=%v", markers, injected)
		}
	}
}

func TestWorkerMiddlewaresKeepNilWorkerBehavior(t *testing.T) {
	for _, emptyFactory := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty-factory-%v", emptyFactory), func(t *testing.T) {
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			if reg.WorkerConfiguration().WorkerMiddlewares != nil {
				t.Fatal("legacy Registry unexpectedly supplies host handlers")
			}
			requests := make(chan workerMiddlewareRequest, 4)
			callers := make(chan AgentIdentity, 1)
			config := WorkerConfig{ModelBuilder: workerMiddlewareBuilder("plain", requests, nil), SubAgentTools: []tool.BaseTool{
				&ctlTool{name: "plain_surface", desc: "observe real tool dispatch", fn: func(ctx context.Context, _ string) (string, error) {
					identity, _ := ToolCaller(ctx)
					select {
					case callers <- identity:
					default:
						t.Error("ordinary worker exceeded its tool-call budget")
					}
					return "tool completed", nil
				}},
			}}
			var factories atomic.Int32
			if emptyFactory {
				config.WorkerMiddlewares = func(AgentIdentity) []adk.ChatModelAgentMiddleware { factories.Add(1); return nil }
			}
			reg.ConfigureWorkers(config)
			h := workerMiddlewareControl(t, reg, "spawn_agent", `{"role":"plain","task":"perform assigned work"}`)
			waitDelegationDone(t, h)
			if result, err, done := h.Result(); !done || err != nil || result != "complete:plain" {
				t.Fatalf("ordinary completion changed: %q %v %v", result, err, done)
			}
			identity := AgentIdentity{AgentID: h.ID, Role: "plain", Generation: 1}
			assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{workerMiddlewareRequestAt(t, requests), workerMiddlewareRequestAt(t, requests)}, "stream", "plain", identity, false)
			if got := workerMiddlewareValueAt(t, callers, "ordinary tool identity"); got != identity {
				t.Fatalf("Injector no longer binds tool identity: %+v", got)
			}
			wantFactories := int32(0)
			if emptyFactory {
				wantFactories = 1
			}
			if factories.Load() != wantFactories {
				t.Fatalf("factory invocations=%d want=%d", factories.Load(), wantFactories)
			}
		})
	}
}

// Reconfiguration occurs while the first real model request is blocked. Both
// its next model round and its resumed generation must retain the old host.
func TestWorkerMiddlewaresRetainTheirGenerationConfiguration(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	requests := make(chan workerMiddlewareRequest, 8)
	factories := make(chan workerMiddlewareInvocation, 4)
	callers := make(chan AgentIdentity, 4)
	release := make(chan struct{})
	configuration := func(name string, gate <-chan struct{}) WorkerConfig {
		return WorkerConfig{ModelBuilder: workerMiddlewareBuilder(name, requests, gate), WorkerMiddlewares: workerMiddlewareFactory(t, name, factories), SubAgentTools: []tool.BaseTool{
			&ctlTool{name: name + "_surface", desc: "retain the selected tool", fn: func(ctx context.Context, _ string) (string, error) {
				identity, _ := ToolCaller(ctx)
				select {
				case callers <- identity:
				default:
					t.Error("configured worker exceeded its tool-call budget")
				}
				return "tool completed", nil
			}},
		}}
	}
	reg.ConfigureWorkers(configuration("original", release))
	h := workerMiddlewareControl(t, reg, "spawn_agent", `{"role":"reader","task":"perform original work"}`)
	first := workerMiddlewareRequestAt(t, requests)
	reg.ConfigureWorkers(configuration("replacement", nil))
	close(release)
	waitDelegationDone(t, h)
	assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{first, workerMiddlewareRequestAt(t, requests)}, "stream", "original", AgentIdentity{AgentID: h.ID, Role: "reader", Generation: 1}, true)
	h = workerMiddlewareControl(t, reg, "resume_agent", fmt.Sprintf(`{"agent_id":%q,"task":"continue original work"}`, h.ID))
	waitDelegationDone(t, h)
	assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{workerMiddlewareRequestAt(t, requests), workerMiddlewareRequestAt(t, requests)}, "stream", "original", AgentIdentity{AgentID: h.ID, Role: "reader", Generation: 2}, true)
	latest := workerMiddlewareControl(t, reg, "spawn_agent", `{"role":"latest","task":"perform new work"}`)
	waitDelegationDone(t, latest)
	assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{workerMiddlewareRequestAt(t, requests), workerMiddlewareRequestAt(t, requests)}, "stream", "replacement", AgentIdentity{AgentID: latest.ID, Role: "latest", Generation: 1}, true)
	for _, want := range []workerMiddlewareInvocation{
		{factory: "original", identity: AgentIdentity{AgentID: h.ID, Role: "reader", Generation: 1}},
		{factory: "original", identity: AgentIdentity{AgentID: h.ID, Role: "reader", Generation: 2}},
		{factory: "replacement", identity: AgentIdentity{AgentID: latest.ID, Role: "latest", Generation: 1}},
	} {
		select {
		case got := <-factories:
			if got != want {
				t.Fatalf("factory identity=%+v want=%+v", got, want)
			}
		default:
			t.Fatalf("missing factory invocation %+v", want)
		}
		if got := workerMiddlewareValueAt(t, callers, "configured tool identity"); got != want.identity {
			t.Fatalf("host handlers replaced Injector provenance: %+v want=%+v", got, want.identity)
		}
	}
	if len(factories) != 0 || len(callers) != 0 {
		t.Fatal("a factory was rebuilt for a model round instead of a worker generation")
	}
}

func TestWorkerMiddlewaresRestoreFromOriginalTag(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	requests := make(chan workerMiddlewareRequest, 4)
	factories := make(chan workerMiddlewareInvocation, 2)
	original := WorkerConfig{Tag: "original-turn", ModelBuilder: func(string, string) model.BaseChatModel {
		return &workerMiddlewareProbe{name: "original", requests: requests}
	}, WorkerMiddlewares: workerMiddlewareFactory(t, "original", factories)}
	reg.ConfigureWorkers(WorkerConfig{Tag: "later-turn", ModelBuilder: finishedBindingBuilder, WorkerMiddlewares: workerMiddlewareFactory(t, "replacement", factories)})
	reg.ResolveWorkerConfig = func(_ context.Context, tag string) (WorkerConfig, error) {
		if tag != original.Tag {
			return WorkerConfig{}, fmt.Errorf("unexpected owner %q", tag)
		}
		return original, nil
	}
	h, err := reg.Restore(t.Context(), RestoredWorker{ID: "reader-9", Role: "reader", Task: "restore assigned work", ConfigTag: original.Tag, ParentID: "parent-1", Generation: 7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitDelegationDone(t, h)
	want := AgentIdentity{AgentID: h.ID, Role: "reader", ParentID: "parent-1", Generation: 7}
	assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{workerMiddlewareRequestAt(t, requests)}, "stream", "original", want, true)
	if got := workerMiddlewareValueAt(t, factories, "restored factory identity"); got != (workerMiddlewareInvocation{factory: "original", identity: want}) || len(factories) != 0 {
		t.Fatalf("restored factory identity=%+v want=%+v", got, want)
	}
}

// Registry workers intentionally stream. Use a separate real ADK runner for
// Generate, rather than counting a fake Stream method that calls Generate.
func TestWorkerHostMiddlewareSupportsActualADKGenerateAndStream(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming-%v", streaming), func(t *testing.T) {
			requests := make(chan workerMiddlewareRequest, 1)
			factories := make(chan workerMiddlewareInvocation, 1)
			identity := AgentIdentity{AgentID: "reader-1", Role: "reader", Generation: 1}
			config := WorkerConfig{WorkerMiddlewares: workerMiddlewareFactory(t, "host", factories)}
			agent, err := adk.NewChatModelAgent(t.Context(), &adk.ChatModelAgentConfig{Name: identity.AgentID, Model: &workerMiddlewareProbe{name: "host", requests: requests}, Handlers: config.WorkerMiddlewares(identity)})
			if err != nil {
				t.Fatal(err)
			}
			iterator := adk.NewRunner(t.Context(), adk.RunnerConfig{Agent: agent, EnableStreaming: streaming}).Run(t.Context(), []*schema.Message{schema.UserMessage("perform assigned work")})
			for {
				event, ok := iterator.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.IsStreaming {
					stream := event.Output.MessageOutput.MessageStream
					for {
						_, err := stream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			request := workerMiddlewareRequestAt(t, requests)
			want := "generate"
			if streaming {
				want = "stream"
			}
			if request.method != want {
				t.Fatalf("actual ADK endpoint=%s want=%s", request.method, want)
			}
			assertWorkerMiddlewareRequests(t, []workerMiddlewareRequest{request}, want, "host", identity, true)
		})
	}
}

package engine

import (
	"context"
	"io"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestToolOfferAllowedFollowsLiveSwitches(t *testing.T) {
	cfg := config.Default()
	e := &Engine{cfg: cfg}
	list, call := mcpclient.ProxyNames("desk")
	if !e.toolOfferAllowed("read") || e.toolOfferAllowed("python_runner") {
		t.Fatalf("catalog defaults read=%t python=%t", e.toolOfferAllowed("read"), e.toolOfferAllowed("python_runner"))
	}
	cfg.Tools.Disabled = []string{"read"}
	cfg.Tools.Enabled = []string{"python_runner"}
	cfg.MCP.Servers = []config.MCPServer{{ID: "desk", Enabled: false}}
	if e.toolOfferAllowed("read") || !e.toolOfferAllowed("python_runner") || e.toolOfferAllowed(list) || e.toolOfferAllowed(call) || !e.toolOfferAllowed("ask_user") {
		t.Fatalf("read=%t python=%t list=%t call=%t ask=%t", e.toolOfferAllowed("read"), e.toolOfferAllowed("python_runner"), e.toolOfferAllowed(list), e.toolOfferAllowed(call), e.toolOfferAllowed("ask_user"))
	}
}

func TestLiveToolOfferDropsAndRestoresOnTheNextModelRequest(t *testing.T) {
	read := &schema.ToolInfo{Name: "read"}
	ask := &schema.ToolInfo{Name: "ask_user"}
	on := true
	offer := &liveToolOffer{allow: func(name string) bool { return name != "read" || on }}
	state := &adk.ChatModelAgentState{ToolInfos: []*schema.ToolInfo{read, ask, nil}, DeferredToolInfos: []*schema.ToolInfo{read}}

	_, next, err := offer.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if names := toolInfoNames(next.ToolInfos); names != "read,ask_user" || toolInfoNames(next.DeferredToolInfos) != "read" {
		t.Fatalf("checked request tools=%q deferred=%q", names, toolInfoNames(next.DeferredToolInfos))
	}
	on = false
	if _, next, err = offer.BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	} else if toolInfoNames(next.ToolInfos) != "ask_user" || toolInfoNames(next.DeferredToolInfos) != "" {
		t.Fatalf("unchecked request tools=%q deferred=%q", toolInfoNames(next.ToolInfos), toolInfoNames(next.DeferredToolInfos))
	}
	on = true
	if _, next, err = offer.BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	} else if toolInfoNames(next.ToolInfos) != "read,ask_user" || toolInfoNames(next.DeferredToolInfos) != "read" {
		t.Fatalf("rechecked tools=%q deferred=%q", toolInfoNames(next.ToolInfos), toolInfoNames(next.DeferredToolInfos))
	}
	if got := selectOfferedTools(nil, nil, func(string) bool { return false }); got != nil {
		t.Fatal("an agent with no tools must stay unset")
	}
	if _, same, err := (*liveToolOffer)(nil).BeforeModelRewriteState(context.Background(), state, nil); err != nil || same != state {
		t.Fatal(err)
	}
}

func TestSwitchedOffToolDoesNotRun(t *testing.T) {
	offer := &liveToolOffer{allow: func(name string) bool { return name != "web_search" }}
	called := false
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		called = true
		return "ran", nil
	}
	denied, err := offer.WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "web_search"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := denied(context.Background(), `{}`)
	if err != nil || called || !strings.Contains(got, `"code":"tool_switched_off"`) || !strings.Contains(got, `"tool":"web_search"`) {
		t.Fatalf("denied=%s called=%t err=%v", got, called, err)
	}
	allowed, err := offer.WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err = allowed(context.Background(), `{}`); err != nil || !called || got != "ran" {
		t.Fatalf("allowed=%s called=%t err=%v", got, called, err)
	}
	streamed := false
	streamNext := func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		streamed = true
		return schema.StreamReaderFromArray([]string{"ran"}), nil
	}
	deniedStream, err := offer.WrapStreamableToolCall(context.Background(), streamNext, &adk.ToolContext{Name: "web_search"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := deniedStream(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := reader.Recv()
	if err != nil || streamed || !strings.Contains(chunk, "tool_switched_off") {
		t.Fatalf("stream denied=%s streamed=%t err=%v", chunk, streamed, err)
	}
	if _, err = reader.Recv(); err != io.EOF {
		t.Fatalf("stream end=%v", err)
	}
	kept, err := offer.WrapStreamableToolCall(context.Background(), streamNext, &adk.ToolContext{Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err = kept(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if chunk, err = reader.Recv(); err != nil || !streamed || chunk != "ran" {
		t.Fatalf("stream kept=%s streamed=%t err=%v", chunk, streamed, err)
	}
}

func TestWorkerOfferFollowsAToolUncheckedDuringTheTurn(t *testing.T) {
	e := newTestEngine(t)
	set, err := tools.Build(t.Context(), e.cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(nil, set, nil, &store.Turn{ID: "turn", ThreadID: "thread"})
	var offer *liveToolOffer
	for _, handler := range reg.WorkerConfiguration().WorkerMiddlewares(swarm.AgentIdentity{}) {
		if got, ok := handler.(*liveToolOffer); ok {
			offer = got
		}
	}
	if offer == nil {
		t.Fatal("worker model requests have no tool switch")
	}
	state := &adk.ChatModelAgentState{ToolInfos: []*schema.ToolInfo{{Name: "read"}, {Name: "exec"}}}
	if _, next, err := offer.BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(toolInfoNames(next.ToolInfos), "read") {
		t.Fatalf("worker lost read before the switch: %s", toolInfoNames(next.ToolInfos))
	}
	if err := e.cfg.Update(func(cfg *config.Config) error {
		cfg.Tools.Disabled = append(cfg.Tools.Disabled, "read")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, next, err := offer.BeforeModelRewriteState(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	} else if names := toolInfoNames(next.ToolInfos); strings.Contains(names, "read") || !strings.Contains(names, "exec") {
		t.Fatalf("worker request after unchecking read: %s", names)
	}
}

func TestMissingToolSwitchStaysOn(t *testing.T) {
	if !(*Engine)(nil).toolOfferAllowed("read") || !(&Engine{}).toolOfferAllowed("read") {
		t.Fatal("a missing configuration must not hide tools")
	}
	if !(*liveToolOffer)(nil).allows(&adk.ToolContext{Name: "read"}) || !(&liveToolOffer{}).allows(nil) || !(&liveToolOffer{}).allows(&adk.ToolContext{}) {
		t.Fatal("a missing switch must not refuse the call")
	}
	known := []*schema.ToolInfo{nil, {Name: ""}, {Name: "read"}}
	if names := toolInfoNames(selectOfferedTools([]*schema.ToolInfo{}, &known, nil)); names != "read" {
		t.Fatalf("remembered holes: %q", names)
	}
	if got := selectOfferedTools([]*schema.ToolInfo{}, nil, func(string) bool { return false }); got == nil || len(got) != 0 {
		t.Fatalf("an empty tool list must stay empty: %#v", got)
	}
}

func toolInfoNames(infos []*schema.ToolInfo) string {
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		if info != nil && info.Name != "" {
			names = append(names, info.Name)
		}
	}
	return strings.Join(names, ",")
}

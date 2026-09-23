package swarm

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ---------- a model that really streams ----------

// turnScript is one model turn expressed as the chunks it streams.
type turnScript struct {
	reasoning []string
	content   []string
	calls     []schema.ToolCall
	// inspect receives the messages the model was called with, so a test can
	// assert on injected steering / inherited context.
	inspect func(msgs []*schema.Message)
}

// chunkedModel streams each scripted turn chunk by chunk, which is what
// exposes accumulation bugs that a single-chunk fake hides.
type chunkedModel struct {
	mu    sync.Mutex
	turns []turnScript
	seen  int
}

func (m *chunkedModel) next(input []*schema.Message) turnScript {
	m.mu.Lock()
	i := m.seen
	m.seen++
	m.mu.Unlock()
	if i >= len(m.turns) {
		i = len(m.turns) - 1
	}
	t := m.turns[i]
	if t.inspect != nil {
		t.inspect(input)
	}
	return t
}

func (m *chunkedModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	t := m.next(input)
	return schema.AssistantMessage(strings.Join(t.content, ""), t.calls), nil
}

func (m *chunkedModel) Stream(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	t := m.next(input)
	sr, sw := schema.Pipe[*schema.Message](len(t.reasoning) + len(t.content) + 2)
	go func() {
		defer sw.Close()
		for _, rc := range t.reasoning {
			sw.Send(&schema.Message{Role: schema.Assistant, ReasoningContent: rc}, nil)
		}
		for _, c := range t.content {
			sw.Send(&schema.Message{Role: schema.Assistant, Content: c}, nil)
		}
		if len(t.calls) > 0 {
			sw.Send(&schema.Message{Role: schema.Assistant, ToolCalls: t.calls}, nil)
		}
	}()
	return sr, nil
}

// recorder collects notifications for assertions.
type recorder struct {
	mu    sync.Mutex
	notes []Notification
}

func (r *recorder) cb() Callback {
	return func(n Notification) {
		r.mu.Lock()
		r.notes = append(r.notes, n)
		r.mu.Unlock()
	}
}

func (r *recorder) all() []Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Notification, len(r.notes))
	copy(out, r.notes)
	return out
}

func (r *recorder) ofKind(k NotifyKind) []Notification {
	var out []Notification
	for _, n := range r.all() {
		if n.Kind == k {
			out = append(out, n)
		}
	}
	return out
}

// fnTool is an inline tool built from a closure.
type fnTool struct {
	name string
	fn   func(ctx context.Context, args string) (string, error)
}

func (t *fnTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.name,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}

func (t *fnTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	return t.fn(ctx, args)
}

// ---------- tests ----------

// A streamed answer must be reported as clean prefixes of itself; the whole
// point of "accumulated text" deltas is that a UI can overwrite its pane.
func TestStreamDeltasAreCleanPrefixes(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{
			reasoning: []string{"let ", "me ", "think"},
			content:   []string{"Hel", "lo ", "world"},
		}}}
	}
	res, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "be terse", Task: "greet"}, rec.cb())
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if res.Final != "Hello world" {
		t.Fatalf("final=%q want %q", res.Final, "Hello world")
	}

	deltas := rec.ofKind(NotifyDelta)
	want := []string{"Hel", "Hello ", "Hello world"}
	if len(deltas) != len(want) {
		t.Fatalf("got %d deltas, want %d: %+v", len(deltas), len(want), deltas)
	}
	for i, n := range deltas {
		if n.Text != want[i] {
			t.Fatalf("delta[%d]=%q want %q", i, n.Text, want[i])
		}
	}
	reasoning := rec.ofKind(NotifyReasoningDelta)
	if len(reasoning) == 0 || reasoning[len(reasoning)-1].Text != "let me think" {
		t.Fatalf("reasoning accumulation broken: %+v", reasoning)
	}
}

// A gateway that resends the whole buffer in each chunk must not glue those
// copies together. Short token repeats still concatenate: that is the model
// saying the token twice, not a snapshot echo.
func TestAbsorbChunkKeepsFragmentsAndDropsResentBuffers(t *testing.T) {
	if got := absorbChunk("Hel", "lo "); got != "Hello " {
		t.Fatalf("fragment=%q", got)
	}
	if got := absorbChunk("a", "a"); got != "aa" {
		t.Fatalf("one-rune repeat=%q", got)
	}
	if got := absorbChunk("ok", "ok"); got != "okok" {
		t.Fatalf("short repeat=%q", got)
	}
	if got := absorbChunk("Hel", "Hello"); got != "Hello" {
		t.Fatalf("snapshot=%q", got)
	}
	resent := "status update from the run"
	if utf8.RuneCountInString(resent) < resentMinRunes {
		t.Fatalf("fixture shorter than the resend threshold: %d", utf8.RuneCountInString(resent))
	}
	if got := absorbChunk(resent, resent); got != resent {
		t.Fatalf("resent buffer=%q", got)
	}
	if got := absorbChunk("", "Hel"); got != "Hel" {
		t.Fatalf("empty prev=%q", got)
	}
	if got := absorbChunk("Hel", ""); got != "Hel" {
		t.Fatalf("empty chunk=%q", got)
	}
}

func TestCumulativeStreamChunksDoNotRepeatTheAnswer(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{
			reasoning: []string{"think ", "think it through", "think it through"},
			content:   []string{"status ", "status update", "status update"},
		}}}
	}
	res, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "be terse", Task: "report"}, rec.cb())
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if res.Final != "status update" {
		t.Fatalf("final=%q", res.Final)
	}
	deltas := rec.ofKind(NotifyDelta)
	if len(deltas) != 2 || deltas[0].Text != "status " || deltas[1].Text != "status update" {
		t.Fatalf("deltas=%v", textsOf(deltas))
	}
	if strings.Count(deltas[1].Text, "status update") != 1 {
		t.Fatalf("snapshot glued on: %q", deltas[1].Text)
	}
	reasoning := rec.ofKind(NotifyReasoningDelta)
	if len(reasoning) != 2 || reasoning[1].Text != "think it through" {
		t.Fatalf("reasoning=%v", textsOf(reasoning))
	}
}

func textsOf(notes []Notification) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.Text
	}
	return out
}

// Each turn starts a fresh accumulator: turn 2's delta must not carry turn 1's
// text, or the UI shows the previous turn's answer glued to the new one.
func TestAccumulatorResetsPerTurn(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{
				reasoning: []string{"first thought"},
				content:   []string{"checking"},
				calls:     []schema.ToolCall{rawCall("tc-1", "probe", "{}")},
			},
			{
				reasoning: []string{"second thought"},
				content:   []string{"all ", "done"},
			},
		}}
	}
	probe := &fnTool{name: "probe", fn: func(ctx context.Context, _ string) (string, error) {
		return "probe result", nil
	}}
	res, err := reg.RunWith(context.Background(), RunConfig{
		Instruction:  "use the probe",
		Task:         "probe it",
		ManagerTools: []tool.BaseTool{probe},
	}, rec.cb())
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if res.Final != "all done" {
		t.Fatalf("final=%q want %q", res.Final, "all done")
	}
	for _, n := range rec.ofKind(NotifyDelta) {
		if strings.Contains(n.Text, "checking") && strings.Contains(n.Text, "done") {
			t.Fatalf("turn 2 delta leaked turn 1 text: %q", n.Text)
		}
	}
	for _, n := range rec.ofKind(NotifyReasoningDelta) {
		if strings.Contains(n.Text, "first") && strings.Contains(n.Text, "second") {
			t.Fatalf("turn 2 reasoning leaked turn 1 text: %q", n.Text)
		}
	}
	if turns := rec.ofKind(NotifyTurn); len(turns) != 2 {
		t.Fatalf("want 2 turn boundaries, got %d", len(turns))
	}
}

// A tool result is useless to a UI unless it can be attached to the call that
// produced it — agents issue several calls per turn.
func TestManagerToolResultPairsByCallID(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{calls: []schema.ToolCall{
				rawCall("call-a", "probe", `{"n":1}`),
				rawCall("call-b", "probe", `{"n":2}`),
			}},
			{content: []string{"finished"}},
		}}
	}
	probe := &fnTool{name: "probe", fn: func(ctx context.Context, args string) (string, error) {
		return "result for " + args, nil
	}}
	if _, err := reg.RunWith(context.Background(), RunConfig{
		Instruction:  "probe twice",
		Task:         "go",
		ManagerTools: []tool.BaseTool{probe},
	}, rec.cb()); err != nil {
		t.Fatalf("RunWith: %v", err)
	}

	calls := rec.ofKind(NotifyToolCall)
	results := rec.ofKind(NotifyToolResult)
	if len(calls) != 2 {
		t.Fatalf("want 2 tool_call notifications, got %d: %+v", len(calls), calls)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 tool_result notifications, got %d: %+v", len(results), results)
	}
	ids := map[string]bool{}
	for _, n := range calls {
		if n.ToolCallID == "" {
			t.Fatalf("tool_call without ToolCallID: %+v", n)
		}
		ids[n.ToolCallID] = true
	}
	for _, n := range results {
		if !ids[n.ToolCallID] {
			t.Fatalf("tool_result %q does not pair with any call %v", n.ToolCallID, ids)
		}
	}
}

// The transcript is what makes a second turn possible, so it must contain the
// conversation the model actually saw, ending with the answer it produced.
func TestRunWithTranscriptFeedsNextTurn(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"the number is 7"}}}}
	}
	first, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "remember things", Task: "pick a number"}, nil)
	if err != nil {
		t.Fatalf("first RunWith: %v", err)
	}
	if len(first.Transcript) == 0 {
		t.Fatal("empty transcript")
	}
	if first.Transcript[0].Role != schema.System || first.Transcript[0].Content != "remember things" {
		t.Fatalf("transcript should start with the instruction: %+v", first.Transcript[0])
	}
	last := first.Transcript[len(first.Transcript)-1]
	if last.Role != schema.Assistant || last.Content != "the number is 7" {
		t.Fatalf("transcript should end with the answer: %+v", last)
	}

	// second turn: replay everything but the system message, plus a follow-up
	var seen []*schema.Message
	reg2 := NewRegistry()
	reg2.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{
			content: []string{"still 7"},
			inspect: func(msgs []*schema.Message) { seen = msgs },
		}}}
	}
	history := append([]adk.Message{}, first.Transcript[1:]...)
	history = append(history, schema.UserMessage("what was it again?"))
	second, err := reg2.RunWith(context.Background(),
		RunConfig{Instruction: "remember things", Messages: history}, nil)
	if err != nil {
		t.Fatalf("second RunWith: %v", err)
	}
	if second.Final != "still 7" {
		t.Fatalf("final=%q", second.Final)
	}
	var joined strings.Builder
	for _, m := range seen {
		joined.WriteString(m.Content)
		joined.WriteString("\n")
	}
	for _, want := range []string{"pick a number", "the number is 7", "what was it again?"} {
		if !strings.Contains(joined.String(), want) {
			t.Fatalf("second turn did not see %q; saw:\n%s", want, joined.String())
		}
	}
}

func TestRunWithMaxIterationsKeepsToolResultsInTranscript(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{
			content: []string{"working"},
			calls:   []schema.ToolCall{rawCall("tc-1", "slow", "{}")},
		}}}
	}
	res, err := reg.RunWith(context.Background(), RunConfig{
		Instruction:   "do work",
		Task:          "start",
		MaxIterations: 1,
		ManagerTools:  []tool.BaseTool{&fnTool{name: "slow", fn: func(context.Context, string) (string, error) { return "slow done", nil }}},
	}, nil)
	if err == nil {
		t.Fatal("want eino's iteration cap")
	}
	var sawTool bool
	for _, m := range res.Transcript {
		if m != nil && m.Role == schema.Tool && m.ToolCallID == "tc-1" && strings.Contains(m.Content, "slow done") {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatalf("the capped transcript must keep the tool result: %+v", res.Transcript)
	}
}

// SteerManager is the manager-side twin of send_message: guidance lands at the
// next turn boundary, never mid-tool.
func TestSteerManagerLandsAtTurnBoundary(t *testing.T) {
	reg := NewRegistry()
	var sawSteer []string
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{
				content: []string{"working"},
				calls:   []schema.ToolCall{rawCall("tc-1", "slow", "{}")},
			},
			{
				content: []string{"acknowledged"},
				inspect: func(msgs []*schema.Message) {
					for _, m := range msgs {
						if m.Role == schema.User && strings.HasPrefix(m.Content, "[steer] ") {
							sawSteer = append(sawSteer, m.Content)
						}
					}
				},
			},
		}}
	}
	// steering from inside the tool proves delivery happens at the boundary
	// after the in-flight tool call, not during it.
	slow := &fnTool{name: "slow", fn: func(ctx context.Context, _ string) (string, error) {
		if !reg.SteerManager("prefer the short answer") {
			t.Error("SteerManager returned false on a live registry")
		}
		return "slow done", nil
	}}
	if _, err := reg.RunWith(context.Background(), RunConfig{
		Instruction:  "do work",
		Task:         "go",
		ManagerTools: []tool.BaseTool{slow},
	}, nil); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(sawSteer) != 1 || !strings.Contains(sawSteer[0], "prefer the short answer") {
		t.Fatalf("steering not delivered to the manager: %+v", sawSteer)
	}
	reg.Close()
	if reg.SteerManager("too late") {
		t.Fatal("SteerManager must refuse on a closed registry")
	}
}

// Worker notifications must reach the UI sink installed by RunWith, with the
// worker's own agent id — that is how the swarm panel gets populated.
func TestWorkerNotificationsReachSink(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{calls: []schema.ToolCall{
					rawCall("s1", "spawn_agent", `{"role":"reader","task":"read it"}`),
				}},
				{calls: []schema.ToolCall{
					rawCall("w1", "wait_agents", `{"agent_ids":["reader-1"],"timeout_s":5}`),
				}},
				{content: []string{"all workers reported"}},
			}}
		}
		return &chunkedModel{turns: []turnScript{{
			reasoning: []string{"worker ", "thinking"},
			content:   []string{"worker ", "answer"},
		}}}
	}
	res, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "delegate", Task: "go"}, rec.cb())
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if res.Final != "all workers reported" {
		t.Fatalf("final=%q", res.Final)
	}

	spawned := rec.ofKind(NotifySpawned)
	if len(spawned) != 1 || spawned[0].Role != "reader" {
		t.Fatalf("want one spawned reader: %+v", spawned)
	}
	var workerDelta, workerFinished bool
	for _, n := range rec.all() {
		if n.AgentID == "reader-1" && n.Kind == NotifyDelta && n.Text == "worker answer" {
			workerDelta = true
		}
		if n.AgentID == "reader-1" && n.Kind == NotifyFinished {
			if n.Text != "worker answer" {
				t.Fatalf("worker result=%q want %q", n.Text, "worker answer")
			}
			workerFinished = true
		}
	}
	if !workerDelta {
		t.Fatalf("missing accumulated worker delta: %+v", rec.all())
	}
	if !workerFinished {
		t.Fatalf("missing worker finished notification: %+v", rec.all())
	}
}

// A run needs something to run on; failing loudly beats sending an empty turn
// to a paid endpoint.
func TestRunWithRejectsEmptyInput(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"never"}}}}
	}
	if _, err := reg.RunWith(context.Background(), RunConfig{Instruction: "x"}, nil); err == nil {
		t.Fatal("want an error when neither Messages nor Task is set")
	}
	noModel := NewRegistry()
	if _, err := noModel.RunWith(context.Background(), RunConfig{Task: "x"}, nil); err == nil {
		t.Fatal("want an error when no model can be built")
	}
}

// RunConfig.Model must win over ModelBuilder so a host can route the manager
// and the workers to different models.
func TestRunConfigModelOverride(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"from builder"}}}}
	}
	res, err := reg.RunWith(context.Background(), RunConfig{
		Instruction: "x",
		Task:        "y",
		Model:       &chunkedModel{turns: []turnScript{{content: []string{"from override"}}}},
	}, nil)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if res.Final != "from override" {
		t.Fatalf("final=%q want the override model's answer", res.Final)
	}
}

// A UI implementation gets OnDone exactly once, with the error on failure.
func TestUIOnDoneOnError(t *testing.T) {
	u := &countingUI{}
	reg := NewRegistry()
	if _, err := reg.RunWith(context.Background(), RunConfig{Task: "x"}, u); err == nil {
		t.Fatal("want an error")
	}
	if u.dones != 1 || u.err == nil {
		t.Fatalf("OnDone calls=%d err=%v", u.dones, u.err)
	}
}

type countingUI struct {
	notes int
	dones int
	err   error
}

func (u *countingUI) OnNotify(Notification)        { u.notes++ }
func (u *countingUI) OnDone(final string, e error) { u.dones++; u.err = e }

func TestNotifyKindRoundTrip(t *testing.T) {
	kinds := []NotifyKind{
		NotifyAgentMessage, NotifySpawned, NotifyFinished, NotifyToolCall,
		NotifyToolResult, NotifyTurn, NotifyDelta, NotifyReasoningDelta,
		NotifyToolDelta, NotifyDone, NotifyError,
	}
	for _, k := range kinds {
		got, ok := ParseNotifyKind(k.String())
		if !ok || got != k {
			t.Fatalf("round trip failed for %v: got %v ok=%v", k, got, ok)
		}
	}
	if _, ok := ParseNotifyKind("nope"); ok {
		t.Fatal("unknown name must not parse")
	}
	if NotifyKind(999).String() != "unknown" {
		t.Fatalf("unexpected name for out-of-range kind")
	}
}

func TestWrapInvokableToolCallEmitsToolDeltaFromBinder(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.setSink(rec.cb())
	reg.ToolOutputBinder = func(ctx context.Context, emit func(string), name, callID string) context.Context {
		if name != "exec" || callID != "c1" {
			t.Errorf("binder got name=%q call=%q", name, callID)
		}
		emit(`{"stdout":"a","stderr":""}`)
		return ctx
	}
	mw := reg.ManagerMiddleware().(*historyRecorder)
	wrapped, err := mw.WrapInvokableToolCall(context.Background(),
		func(ctx context.Context, args string, _ ...tool.Option) (string, error) {
			return `{"stdout":"ab","stderr":""}`, nil
		}, &adk.ToolContext{Name: "exec", CallID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := wrapped(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"stdout":"ab"`) {
		t.Fatalf("final result: %s", out)
	}
	deltas := rec.ofKind(NotifyToolDelta)
	if len(deltas) != 1 || deltas[0].Text != `{"stdout":"a","stderr":""}` || deltas[0].ToolCallID != "c1" {
		t.Fatalf("tool_delta: %+v", deltas)
	}
	if deltas[0].AgentID != DefaultManagerID {
		t.Fatalf("manager delta on %q", deltas[0].AgentID)
	}
}

func TestWorkerInjectorEmitsToolDeltaFromBinder(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.setSink(rec.cb())
	reg.ToolOutputBinder = func(ctx context.Context, emit func(string), name, callID string) context.Context {
		emit(`{"stdout":"w","stderr":""}`)
		return ctx
	}
	h := &Handle{ID: "worker-1", Role: "researcher"}
	inj := &Injector{handle: h, reg: reg}
	wrapped, err := inj.WrapInvokableToolCall(context.Background(),
		func(ctx context.Context, args string, _ ...tool.Option) (string, error) {
			return "ok", nil
		}, &adk.ToolContext{Name: "exec", CallID: "c2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped(context.Background(), `{}`); err != nil {
		t.Fatal(err)
	}
	deltas := rec.ofKind(NotifyToolDelta)
	if len(deltas) != 1 || deltas[0].AgentID != "worker-1" || deltas[0].ToolCallID != "c2" {
		t.Fatalf("worker tool_delta: %+v", deltas)
	}
}

func TestClipToolResultKeepsNewlinesAndCutsOnRunes(t *testing.T) {
	if got := clipToolResult("a\nb", 10); got != "a\nb" {
		t.Fatalf("newlines must stay so a file body can be highlighted: %q", got)
	}
	if got := clipToolResult("中文很长的一段话", 4); got != "中文很长…" {
		t.Fatalf("must cut on runes, not bytes: %q", got)
	}
	long := strings.Repeat("x", 80)
	if got := clipToolResult(long, 10); got != strings.Repeat("x", 10)+"…" {
		t.Fatalf("clip: %q", got)
	}
}

func TestToolResultKeepsTheFileBodyReadable(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	restore := reg.setSink(rec.cb())
	defer reg.setSink(restore)

	body := "encoding=utf-8 path=notes.md offset=1 limit=200\n1|# heading\n2|a short paragraph"
	reg.emitComplete("worker", "reader-1", adk.EventFromMessage(
		schema.ToolMessage(body, "c1"), nil, schema.Tool, ""))

	notes := rec.all()
	if len(notes) != 1 || notes[0].Kind != NotifyToolResult {
		t.Fatalf("want one tool_result, got %+v", notes)
	}
	if notes[0].Text != body {
		t.Fatalf("file body was flattened or truncated:\n got %q\nwant %q", notes[0].Text, body)
	}
}

func TestSummarizeAndMerge(t *testing.T) {
	idx0, idx1 := 0, 1
	merged := mergeStreamedToolCalls([]schema.ToolCall{
		{Index: &idx0, ID: "a", Function: schema.FunctionCall{Name: "f", Arguments: `{"x`}},
		{Index: &idx1, ID: "b", Function: schema.FunctionCall{Name: "g", Arguments: `{"y`}},
		{Index: &idx0, Function: schema.FunctionCall{Name: "f", Arguments: `":1}`}},
		{Index: &idx1, Function: schema.FunctionCall{Arguments: `":2}`}},
	})
	if len(merged) != 2 {
		t.Fatalf("want 2 merged calls, got %d: %+v", len(merged), merged)
	}
	if merged[0].ID != "a" || merged[0].Function.Arguments != `{"x":1}` {
		t.Fatalf("merge[0]=%+v", merged[0])
	}
	if merged[1].ID != "b" || merged[1].Function.Name != "g" || merged[1].Function.Arguments != `{"y":2}` {
		t.Fatalf("merge[1]=%+v", merged[1])
	}
	if mergeStreamedToolCalls(nil) != nil {
		t.Fatal("nil in, nil out")
	}
}

// Non-streaming providers still have to produce a usable event stream, so the
// complete-message path is exercised directly.
func TestEmitCompleteMapsNonStreamingEvents(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	restore := reg.setSink(rec.cb())
	defer reg.setSink(restore)

	reg.emitComplete("worker", "reader-1", adk.EventFromMessage(
		schema.AssistantMessage("plain answer", nil), nil, schema.Assistant, ""))
	reg.emitComplete("worker", "reader-1", adk.EventFromMessage(
		schema.AssistantMessage("calling", []schema.ToolCall{rawCall("c1", "probe", "{}")}),
		nil, schema.Assistant, ""))
	reg.emitComplete("worker", "reader-1", adk.EventFromMessage(
		schema.ToolMessage("tool output", "c1"), nil, schema.Tool, ""))

	// ignored shapes: nil event, error event, and a streaming event
	reg.emitComplete("worker", "reader-1", nil)
	reg.emitComplete("worker", "reader-1", &adk.AgentEvent{Err: context.Canceled})
	reg.emitComplete("worker", "reader-1", &adk.AgentEvent{})

	notes := rec.all()
	if len(notes) != 3 {
		t.Fatalf("want 3 notifications, got %d: %+v", len(notes), notes)
	}
	if notes[0].Kind != NotifyAgentMessage || notes[0].Text != "plain answer" {
		t.Fatalf("notes[0]=%+v", notes[0])
	}
	if notes[1].Kind != NotifyToolCall || notes[1].ToolCallID != "c1" {
		t.Fatalf("notes[1]=%+v", notes[1])
	}
	if notes[2].Kind != NotifyToolResult || notes[2].ToolCallID != "c1" || notes[2].Text != "tool output" {
		t.Fatalf("notes[2]=%+v", notes[2])
	}
	// an assistant message that both speaks and calls a tool reports only the
	// call: the text is commentary the UI attaches to the tool row
	if notes[1].Text == "calling" {
		t.Fatal("commentary must not be emitted as a sealed answer")
	}
}

func TestItoa(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 42: "42", -13: "-13", 1000000: "1000000"} {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d)=%q want %q", in, got, want)
		}
	}
}

func TestAsUIForms(t *testing.T) {
	if asUI(nil) != nil {
		t.Fatal("nil ui")
	}
	if asUI("not a ui") != nil {
		t.Fatal("unknown type must be ignored")
	}
	var got int
	if u := asUI(func(Notification) { got++ }); u == nil {
		t.Fatal("plain func must adapt")
	} else {
		u.OnNotify(Notification{})
		u.OnDone("x", nil)
		if got != 2 {
			t.Fatalf("adapter dropped events: %d", got)
		}
	}
	var errNote Notification
	u := asUI(Callback(func(n Notification) { errNote = n }))
	u.OnDone("boom", context.Canceled)
	if errNote.Kind != NotifyError || errNote.Err == nil {
		t.Fatalf("OnDone with error should notify NotifyError: %+v", errNote)
	}
	if (cbUI{}).OnDone("x", nil); false {
		t.Fatal("unreachable")
	}
}

// Interrupting a turn must still hand back what the manager produced so far,
// otherwise a canceled turn loses its transcript.
func TestRunWithCancelKeepsTranscript(t *testing.T) {
	reg := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{
				content: []string{"starting"},
				calls:   []schema.ToolCall{rawCall("tc-1", "block", "{}")},
			},
			{content: []string{"unreachable"}},
		}}
	}
	block := &fnTool{name: "block", fn: func(ctx context.Context, _ string) (string, error) {
		cancel()
		<-ctx.Done()
		return "", ctx.Err()
	}}
	res, err := reg.RunWith(ctx, RunConfig{
		Instruction:  "x",
		Task:         "y",
		ManagerTools: []tool.BaseTool{block},
	}, nil)
	if err == nil {
		t.Fatal("want a cancellation error")
	}
	if len(res.Transcript) == 0 {
		t.Fatal("a canceled turn must still return its partial transcript")
	}
	found := false
	for _, m := range res.Transcript {
		if m.Role == schema.Assistant && len(m.ToolCalls) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("partial transcript lost the tool-calling turn: %+v", res.Transcript)
	}
}

func TestRunOneShotStillWorks(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"one shot"}}}}
	}
	final, err := reg.RunWithCallback(context.Background(), "go", func(Notification) {})
	if err != nil {
		t.Fatalf("RunWithCallback: %v", err)
	}
	if final != "one shot" {
		t.Fatalf("final=%q", final)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := reg.Run(context.Background(), "go", nil); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

// Steering that arrives after the manager's last model call is never read by
// the middleware. It must still be recoverable, so the caller can decide to run
// it rather than lose what the user typed.
func TestPendingSteersSurviveARunThatNeverReadsThem(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"done here"}}}}
	}
	if _, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "x", Task: "go"}, nil); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	// queued after the only model call, so nothing will ever drain it
	if !reg.SteerManager("also mention the caveats") {
		t.Fatal("SteerManager refused on a live registry")
	}
	pending := reg.TakePendingSteers()
	if len(pending) != 1 || pending[0] != "also mention the caveats" {
		t.Fatalf("pending steers lost: %+v", pending)
	}
	if left := reg.TakePendingSteers(); len(left) != 0 {
		t.Fatalf("taking twice returned %+v", left)
	}
}

func TestWorkerInstructionWithoutPreambleIsTheTask(t *testing.T) {
	r := NewRegistry()
	if got := r.workerInstruction("do it"); got != "do it" {
		t.Fatalf("got %q", got)
	}
}

func TestWorkerPreamblePrefixesTheTask(t *testing.T) {
	r := NewRegistry()
	r.WorkerPreamble = "  OS: testhost  "
	got := r.workerInstruction("do it")
	if got != "OS: testhost\n\ndo it" {
		t.Fatalf("got %q", got)
	}
}

// Workers do not see the manager prompt. Without a preamble they invent the
// wrong userland; the host has to hand those facts over as Instruction.
func TestWorkerPreambleLandsOnTheWorkerSystemPrompt(t *testing.T) {
	var seen []*schema.Message
	reg := NewRegistry()
	reg.WorkerPreamble = "OS: testhost"
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{calls: []schema.ToolCall{
					rawCall("s1", "spawn_agent", `{"role":"reader","task":"read it"}`),
				}},
				{calls: []schema.ToolCall{
					rawCall("w1", "wait_agents", `{"agent_ids":["reader-1"],"timeout_s":5}`),
				}},
				{content: []string{"done"}},
			}}
		}
		return &chunkedModel{turns: []turnScript{{
			content: []string{"ok"},
			inspect: func(msgs []*schema.Message) { seen = msgs },
		}}}
	}
	if _, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "delegate", Task: "go"}, nil); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("the worker was never called")
	}
	var sys string
	for _, m := range seen {
		if m != nil && m.Role == schema.System {
			sys = m.Content
			break
		}
	}
	if !strings.Contains(sys, "OS: testhost") || !strings.Contains(sys, "read it") {
		t.Fatalf("worker system prompt=%q", sys)
	}
}

// The spawned event is the only place a host can recover what a worker was
// actually told. Text used to repeat the role; that made a prompt viewer lie.
func TestSpawnedNotificationCarriesTheWorkerInstruction(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.WorkerPreamble = "OS: testhost"
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{calls: []schema.ToolCall{
					rawCall("s1", "spawn_agent", `{"role":"reader","task":"read it"}`),
				}},
				{calls: []schema.ToolCall{
					rawCall("w1", "wait_agents", `{"agent_ids":["reader-1"],"timeout_s":5}`),
				}},
				{content: []string{"done"}},
			}}
		}
		return &chunkedModel{turns: []turnScript{{content: []string{"ok"}}}}
	}
	if _, err := reg.RunWith(context.Background(),
		RunConfig{Instruction: "delegate", Task: "go"}, rec.cb()); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	spawned := rec.ofKind(NotifySpawned)
	if len(spawned) != 1 || spawned[0].Role != "reader" {
		t.Fatalf("want one spawned reader: %+v", spawned)
	}
	if spawned[0].Text == spawned[0].Role {
		t.Fatal("spawned text is still the role; the instruction never made the event")
	}
	if !strings.Contains(spawned[0].Text, "OS: testhost") || !strings.Contains(spawned[0].Text, "read it") {
		t.Fatalf("spawned instruction=%q", spawned[0].Text)
	}
}

// A /goal session parks the registry when the manager stops calling tools.
// The worker is often still inside a tool; restoring a nil sink used to drop
// its finished event, so Agents froze on "starting" with an empty pane.
func TestHostNotifyKeepsWorkerEventsAfterRunReturns(t *testing.T) {
	rec := &recorder{}
	reg := NewRegistry()
	reg.SetHostNotify(rec.cb())
	released := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(released) })
	reg.SubAgentTools = []tool.BaseTool{&fnTool{name: "work", fn: func(context.Context, string) (string, error) {
		<-released
		return "worked", nil
	}}}
	reg.ModelBuilder = func(role, id string) model.BaseChatModel {
		if id == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{
					content: []string{"fan out"},
					calls: []schema.ToolCall{rawCall("s1", "spawn_agent",
						`{"role":"worker","task":"do the assigned work"}`)},
				},
				{content: []string{"the workers are running; wrapping this turn"}},
			}}
		}
		return &chunkedModel{turns: []turnScript{
			{
				content: []string{"on it"},
				calls:   []schema.ToolCall{rawCall("w1", "work", "{}")},
			},
			{content: []string{"worker finished the assigned work"}},
		}}
	}
	res, err := reg.RunWith(context.Background(), RunConfig{
		Instruction: "coordinate",
		Task:        "split the work",
	}, rec.cb())
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !strings.Contains(res.Final, "wrapping this turn") {
		t.Fatalf("manager should have left without waiting: %q", res.Final)
	}
	if n := len(rec.ofKind(NotifyFinished)); n != 0 {
		t.Fatalf("worker must still be blocked when the manager returns, got %d finished", n)
	}
	release.Do(func() { close(released) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, n := range rec.ofKind(NotifyFinished) {
			if n.AgentID != DefaultManagerID && n.Err == nil {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("parked worker finished into the void: %+v", rec.all())
}

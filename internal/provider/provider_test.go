package provider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func configFor(t *testing.T, ready bool) *config.Config {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if ready {
		cfg.Models.Providers[0].BaseURL = "http://endpoint.invalid/v1"
		cfg.Models.Providers[0].Model = "some-model"
		cfg.Models.Providers[0].APIKey = "k"
	}
	return cfg
}

func TestResolveAndList(t *testing.T) {
	cfg := configFor(t, true)
	cfg.Models.Providers = append(cfg.Models.Providers, config.Provider{
		ID: "blank", Label: "Blank",
	})
	p := New(cfg)

	if _, err := p.Resolve("nope"); err == nil {
		t.Fatal("unknown provider must not resolve")
	}
	def, err := p.Resolve("")
	if err != nil {
		t.Fatalf("empty id should resolve to the default: %v", err)
	}
	if def.ID != cfg.Models.Default {
		t.Fatalf("resolved %q", def.ID)
	}

	list := p.List()
	if len(list) != 2 {
		t.Fatalf("want 2 providers, got %d", len(list))
	}
	byID := map[string]Info{}
	for _, i := range list {
		byID[i.ID] = i
	}
	if !byID[cfg.Models.Default].Ready {
		t.Fatal("a fully configured provider should report ready")
	}
	if byID["blank"].Ready {
		t.Fatal("a provider with no endpoint must not report ready")
	}
	if byID["blank"].Label != "Blank" {
		t.Fatalf("label=%q", byID["blank"].Label)
	}
	if p.IsMock() {
		t.Fatal("a real pool is not a mock")
	}
}

// One client per provider, shared by the whole swarm: a dozen agents on one
// endpoint should mean one connection pool, not a dozen.
func TestGetCachesOneClientPerProvider(t *testing.T) {
	p := New(configFor(t, true))
	first, err := p.Get(context.Background(), "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	second, err := p.Get(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Get built a second client for the same provider")
	}

	// settings changes must take effect without a restart
	p.Invalidate()
	third, err := p.Get(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("Invalidate did not drop the cached client")
	}
}

// Every agent in a run shares the provider's client but gets its own
// telemetry wrapper, so a trace can say which agent made which call.
func TestBuilderSharesClientAndAttributesPerAgent(t *testing.T) {
	s := &sink{}
	p := New(configFor(t, true))
	build, err := p.ModelBuilder(context.Background(), "", s.rec())
	if err != nil {
		t.Fatalf("ModelBuilder: %v", err)
	}
	mgr := build("manager", "manager")
	worker := build("researcher", "researcher-1")
	if mgr == nil || worker == nil {
		t.Fatal("builder returned nil")
	}
	if mgr == worker {
		t.Fatal("each agent needs its own wrapper for attribution")
	}
	mgrInner, ok := mgr.(*recordingModel)
	if !ok {
		t.Fatalf("expected a recording wrapper, got %T", mgr)
	}
	workerInner, ok := worker.(*recordingModel)
	if !ok {
		t.Fatalf("expected a recording wrapper, got %T", worker)
	}
	if mgrInner.inner != workerInner.inner {
		t.Fatal("agents should share one client per provider")
	}
	if mgrInner.agentID != "manager" || workerInner.agentID != "researcher-1" {
		t.Fatalf("attribution wrong: %q / %q", mgrInner.agentID, workerInner.agentID)
	}

	// with no recorder the shared client is handed out bare
	bare, err := p.ModelBuilder(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := bare("manager", "manager").(*recordingModel); wrapped {
		t.Fatal("no recorder should mean no wrapper")
	}
}

func TestBuilderRefusesUnconfiguredProvider(t *testing.T) {
	p := New(configFor(t, false))
	_, err := p.ModelBuilder(context.Background(), "", nil)
	if err == nil {
		t.Fatal("want an error for a provider with no endpoint")
	}
	if !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("the error should point the user at Settings, got %q", err)
	}
	if _, err := p.ModelBuilder(context.Background(), "nope", nil); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	if _, err := p.Get(context.Background(), ""); err == nil {
		t.Fatal("Get must refuse an unconfigured provider")
	}
}

// The mock pool has to work with the config a fresh install has: no endpoint,
// no key, nothing.
func TestMockPoolRunsWithNoConfiguration(t *testing.T) {
	p := NewMock(configFor(t, false))
	if !p.IsMock() {
		t.Fatal("IsMock")
	}
	for _, i := range p.List() {
		if !i.Ready {
			t.Fatalf("mock providers are always ready: %+v", i)
		}
	}
	build, err := p.ModelBuilder(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("ModelBuilder: %v", err)
	}
	if build("manager", "manager") == nil {
		t.Fatal("nil manager model")
	}
	if _, err := p.Get(context.Background(), ""); err != nil {
		t.Fatalf("Get on a mock pool: %v", err)
	}
}

// An offline run is usually against a provider with nothing filled in. The
// turn it produces still has to say what answered it, or its trace reads as
// if the model name was lost.
func TestMockPoolNamesItself(t *testing.T) {
	p := NewMock(configFor(t, false))
	prov, err := p.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if prov.Model != MockModelName {
		t.Fatalf("model=%q want %q", prov.Model, MockModelName)
	}
	if prov.DisplayName() == prov.ID {
		t.Fatalf("the offline provider needs a readable label, got %q", prov.DisplayName())
	}

	var got []CallRecord
	build, err := p.ModelBuilder(context.Background(), "",
		func(r CallRecord) { got = append(got, r) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build("manager", "manager").Generate(context.Background(),
		[]*schema.Message{schema.UserMessage("anything")}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Model != MockModelName {
		t.Fatalf("the recorded call does not name the provider: %+v", got)
	}

	// a real pool reports exactly what is configured, with no substitution
	real := New(configFor(t, true))
	realProv, err := real.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if realProv.Model == MockModelName {
		t.Fatal("a configured provider must not be relabelled as the mock")
	}
}

// The scripted manager must actually drive a swarm: fan out, collect, answer.
func TestMockManagerScriptFansOutAndAnswers(t *testing.T) {
	m := newMockModel("manager")
	ctx := context.Background()

	first, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("summarize the inputs")})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 2 {
		t.Fatalf("want two parallel spawns, got %d", len(first.ToolCalls))
	}
	forked := false
	for _, c := range first.ToolCalls {
		if c.Function.Name != "spawn_agent" {
			t.Fatalf("unexpected tool %q", c.Function.Name)
		}
		if !strings.Contains(c.Function.Arguments, "summarize the inputs") {
			t.Fatalf("spawn task does not carry the request: %s", c.Function.Arguments)
		}
		if strings.Contains(c.Function.Arguments, `"fork_context":true`) {
			forked = true
		}
	}
	if !forked {
		t.Fatal("one worker should inherit the conversation, to exercise fork_context")
	}
	if first.ReasoningContent == "" {
		t.Fatal("the manager should think out loud, so the UI has reasoning to show")
	}

	// turn 2 waits on the ids its own tool results handed back
	convo := []*schema.Message{
		schema.UserMessage("summarize the inputs"),
		first,
		schema.ToolMessage(`{"agent_id":"researcher-1"}`, "mock-spawn-1"),
		schema.ToolMessage(`{"agent_id":"reviewer-2","forked":"true"}`, "mock-spawn-2"),
	}
	second, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ToolCalls) != 1 || second.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("turn 2 should wait for the workers: %+v", second.ToolCalls)
	}
	for _, id := range []string{"researcher-1", "reviewer-2"} {
		if !strings.Contains(second.ToolCalls[0].Function.Arguments, id) {
			t.Fatalf("wait_agents missed %q: %s", id, second.ToolCalls[0].Function.Arguments)
		}
	}

	// turn 3 answers, quoting what the workers reported
	convo = append(convo, second,
		schema.ToolMessage(`[{"agent_id":"researcher-1","result":"researcher done"},`+
			`{"agent_id":"reviewer-2","result":"reviewer done"}]`, "mock-wait-1"))
	third, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.ToolCalls) != 0 {
		t.Fatalf("the final turn must not call tools: %+v", third.ToolCalls)
	}
	for _, want := range []string{"summarize the inputs", "researcher done", "reviewer done", "## Result"} {
		if !strings.Contains(third.Content, want) {
			t.Fatalf("final answer missing %q:\n%s", want, third.Content)
		}
	}
}

func TestMockManagerAnswersEvenWithNoWorkers(t *testing.T) {
	m := newMockModel("manager")
	ctx := context.Background()
	if _, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("q")}); err != nil {
		t.Fatal(err)
	}
	// no spawn results came back: rather than waiting on nothing, answer
	out, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("q")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ToolCalls) != 0 || !strings.Contains(out.Content, "No sub-agent results") {
		t.Fatalf("want a direct answer, got %+v / %q", out.ToolCalls, out.Content)
	}
}

func TestMockWorkerWritesIntoTheWorkspace(t *testing.T) {
	m := newMockModel("researcher")
	ctx := context.Background()
	convo := []*schema.Message{schema.UserMessage("collect the inputs")}

	first, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "write" {
		t.Fatalf("a worker should do real work: %+v", first.ToolCalls)
	}
	args := first.ToolCalls[0].Function.Arguments
	// relative path: it must land in the conversation's workspace, not / or cwd
	if !strings.Contains(args, `"file_path":"notes/researcher.md"`) {
		t.Fatalf("worker write path wrong: %s", args)
	}
	if strings.Contains(args, `"file_path":"/`) {
		t.Fatalf("worker must not write to an absolute path: %s", args)
	}

	second, err := m.Generate(ctx, append(convo, first, schema.ToolMessage("Updated file notes/researcher.md", "x")))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ToolCalls) != 0 || !strings.Contains(second.Content, "researcher finished") {
		t.Fatalf("worker did not report back: %+v / %q", second.ToolCalls, second.Content)
	}
}

// Streaming must reassemble to exactly the scripted text: the UI's accumulated
// deltas depend on chunks being a clean partition of the message.
func TestMockStreamReassemblesExactly(t *testing.T) {
	m := newMockModel("manager")
	ctx := context.Background()
	stream, err := m.Stream(ctx, []*schema.Message{schema.UserMessage("do a thing")})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	var content, reasoning strings.Builder
	var calls []schema.ToolCall
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		content.WriteString(msg.Content)
		reasoning.WriteString(msg.ReasoningContent)
		calls = append(calls, msg.ToolCalls...)
	}

	want, err := newMockModel("manager").Generate(ctx, []*schema.Message{schema.UserMessage("do a thing")})
	if err != nil {
		t.Fatal(err)
	}
	if content.String() != want.Content {
		t.Fatalf("streamed content:\n%q\nwant:\n%q", content.String(), want.Content)
	}
	if reasoning.String() != want.ReasoningContent {
		t.Fatalf("streamed reasoning:\n%q\nwant:\n%q", reasoning.String(), want.ReasoningContent)
	}
	if len(calls) != len(want.ToolCalls) {
		t.Fatalf("streamed %d tool calls, want %d", len(calls), len(want.ToolCalls))
	}
}

func TestMockRespectsCancellation(t *testing.T) {
	m := newMockModel("manager")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("Generate must respect a canceled context")
	}
	if _, err := m.Stream(ctx, []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("Stream must respect a canceled context")
	}
}

// ---------- telemetry ----------

type fakeModel struct {
	out    *schema.Message
	chunks []*schema.Message
	err    error
}

func (f *fakeModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return f.out, f.err
}

func (f *fakeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if f.err != nil {
		return nil, f.err
	}
	sr, sw := schema.Pipe[*schema.Message](len(f.chunks) + 1)
	go func() {
		defer sw.Close()
		for _, c := range f.chunks {
			sw.Send(c, nil)
		}
	}()
	return sr, nil
}

type sink struct {
	mu   sync.Mutex
	recs []CallRecord
}

func (s *sink) rec() Recorder {
	return func(r CallRecord) {
		s.mu.Lock()
		s.recs = append(s.recs, r)
		s.mu.Unlock()
	}
}

func (s *sink) all() []CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CallRecord, len(s.recs))
	copy(out, s.recs)
	return out
}

func TestTelemetryRecordsGenerate(t *testing.T) {
	s := &sink{}
	inner := &fakeModel{out: schema.AssistantMessage("hello", nil)}
	m := wrap(inner, "worker-1", config.Provider{ID: "p", Model: "m"}, s.rec())

	if _, err := m.Generate(context.Background(),
		[]*schema.Message{schema.UserMessage("hi"), schema.UserMessage("there")}); err != nil {
		t.Fatal(err)
	}
	recs := s.all()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.AgentID != "worker-1" || r.ProviderID != "p" || r.Model != "m" {
		t.Fatalf("record not attributed: %+v", r)
	}
	if r.InputMsgs != 2 || r.InputChars != len("hi")+len("there") {
		t.Fatalf("input shape wrong: %+v", r)
	}
	if r.OutputChars != len("hello") {
		t.Fatalf("output size wrong: %+v", r)
	}
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
}

// A streamed call is not over when Stream returns; the record must land when
// the stream is drained, with the real duration and output size.
func TestTelemetryRecordsStreamOnDrain(t *testing.T) {
	s := &sink{}
	inner := &fakeModel{chunks: []*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "think"},
		{Role: schema.Assistant, Content: "one "},
		{Role: schema.Assistant, Content: "two"},
	}}
	m := wrap(inner, "manager", config.Provider{ID: "p", Model: "m"}, s.rec())

	stream, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("go")})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		got.WriteString(msg.Content)
	}
	stream.Close()

	deadline := time.Now().Add(2 * time.Second)
	for len(s.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got.String() != "one two" {
		t.Fatalf("relay corrupted the stream: %q", got.String())
	}
	recs := s.all()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].OutputChars != len("think")+len("one ")+len("two") {
		t.Fatalf("streamed output size wrong: %+v", recs[0])
	}
}

// Abandoning a stream half-way must not leak the relay goroutine, and must
// still produce a record.
func TestTelemetryHandlesAbandonedStream(t *testing.T) {
	s := &sink{}
	chunks := make([]*schema.Message, 200)
	for i := range chunks {
		chunks[i] = &schema.Message{Role: schema.Assistant, Content: "x"}
	}
	m := wrap(&fakeModel{chunks: chunks}, "manager", config.Provider{ID: "p"}, s.rec())

	stream, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("go")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	stream.Close()

	deadline := time.Now().Add(2 * time.Second)
	for len(s.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(s.all()) == 0 {
		t.Fatal("an abandoned stream produced no record; the relay goroutine is stuck")
	}
}

func TestTelemetryRecordsFailures(t *testing.T) {
	s := &sink{}
	boom := errors.New("endpoint refused")
	m := wrap(&fakeModel{err: boom}, "manager", config.Provider{ID: "p"}, s.rec())

	if _, err := m.Generate(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("Generate error not propagated: %v", err)
	}
	if _, err := m.Stream(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("Stream error not propagated: %v", err)
	}
	recs := s.all()
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	for _, r := range recs {
		if r.Err == nil {
			t.Fatalf("a failed call was recorded as a success: %+v", r)
		}
	}
}

// With no recorder there must be no wrapper at all, so an unwatched run pays
// nothing for telemetry.
func TestWrapWithoutRecorderIsIdentity(t *testing.T) {
	inner := &fakeModel{out: schema.AssistantMessage("x", nil)}
	if got := wrap(inner, "a", config.Provider{}, nil); got != model.BaseChatModel(inner) {
		t.Fatal("wrap should return the model untouched when nobody is recording")
	}
}

func TestSafeNameKeepsFileNamesUsable(t *testing.T) {
	for in, want := range map[string]string{
		"researcher":       "researcher",
		"Data Analyst":     "data-analyst",
		"web/scraper":      "web-scraper",
		"../../etc/passwd": "etc-passwd",
		"":                 "worker",
		"!!!":              "worker",
	} {
		if got := safeName(in); got != want {
			t.Fatalf("safeName(%q)=%q want %q", in, got, want)
		}
	}
	for _, in := range []string{"../../etc/passwd", "a/b", "x\\y"} {
		if strings.ContainsAny(safeName(in), "/\\.") {
			t.Fatalf("safeName(%q)=%q is not a safe file name", in, safeName(in))
		}
	}
}

func TestSplitWordsPartitionsExactly(t *testing.T) {
	for _, in := range []string{"", "a", "hello world", "line one\nline two\n", "  spaced  out  "} {
		parts := splitWords(in)
		if strings.Join(parts, "") != in {
			t.Fatalf("splitWords(%q) does not reassemble: %q", in, strings.Join(parts, ""))
		}
	}
	if splitWords("") != nil {
		t.Fatal("empty in, nil out")
	}
}

func TestOneLineTruncatesOnRunes(t *testing.T) {
	long := strings.Repeat("中", 300)
	got := oneLine(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("not truncated: %q", got)
	}
	if len([]rune(got)) != 161 {
		t.Fatalf("truncated on bytes instead of runes: %d runes", len([]rune(got)))
	}
	if got := oneLine(" a \n b "); got != "a b" {
		t.Fatalf("oneLine=%q", got)
	}
}

func TestUserTextExtraction(t *testing.T) {
	if got := lastUserText(nil); got != "(no request)" {
		t.Fatalf("lastUserText=%q", got)
	}
	if got := firstUserText(nil); got != "(no task)" {
		t.Fatalf("firstUserText=%q", got)
	}
	msgs := []*schema.Message{
		schema.UserMessage("first"),
		schema.AssistantMessage("reply", nil),
		schema.UserMessage("[steer] steered text"),
	}
	// steering is the newest instruction, so it is what the mock answers
	if got := lastUserText(msgs); got != "steered text" {
		t.Fatalf("lastUserText=%q", got)
	}
	if got := firstUserText(msgs); got != "first" {
		t.Fatalf("firstUserText=%q", got)
	}
}

func TestWaitedResultsReportsFailures(t *testing.T) {
	msgs := []*schema.Message{
		schema.ToolMessage(`[{"agent_id":"a-1","result":"ok"},{"agent_id":"b-2","error":"timed out"}]`, "x"),
		schema.ToolMessage(`not json`, "y"),
	}
	got := waitedResults(msgs)
	if len(got) != 2 || got[0] != "ok" || !strings.Contains(got[1], "timed out") {
		t.Fatalf("waitedResults=%+v", got)
	}
}

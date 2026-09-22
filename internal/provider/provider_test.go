package provider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
	if len(list) != 1 {
		t.Fatalf("an endpoint with no catalog still lists its default; blank listed %d", len(list))
	}
	if list[0].ProviderID != cfg.Models.Default || !list[0].Ready || !list[0].Default {
		t.Fatalf("default row: %+v", list[0])
	}
	if list[0].ID != ChoiceID(cfg.Models.Default, "some-model") {
		t.Fatalf("id=%q", list[0].ID)
	}
	if p.IsMock() {
		t.Fatal("a real pool is not a mock")
	}
}

// One client per provider, shared by the whole swarm: a dozen agents on one
// endpoint should mean one connection pool, not a dozen.
func TestGetCachesOneClientPerProvider(t *testing.T) {
	p := New(configFor(t, true))
	first, err := p.Get(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	second, err := p.Get(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Get built a second client for the same provider")
	}

	// settings changes must take effect without a restart
	p.Invalidate()
	third, err := p.Get(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("Invalidate did not drop the cached client")
	}

	other, err := p.Get(context.Background(), "", "other-model")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("a different model on the same endpoint needs its own client")
	}
}

// Every agent in a run shares the provider's client but gets its own
// telemetry wrapper, so a trace can say which agent made which call.
func TestBuilderSharesClientAndAttributesPerAgent(t *testing.T) {
	s := &sink{}
	p := New(configFor(t, true))
	build, err := p.ModelBuilder(context.Background(), "", "", "", s.rec())
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
	bare, err := p.ModelBuilder(context.Background(), "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := bare("manager", "manager").(*recordingModel); wrapped {
		t.Fatal("no recorder should mean no wrapper")
	}
}

func TestBuilderRefusesUnconfiguredProvider(t *testing.T) {
	p := New(configFor(t, false))
	_, err := p.ModelBuilder(context.Background(), "", "", "", nil)
	if err == nil {
		t.Fatal("want an error for a provider with no endpoint")
	}
	if !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("the error should point the user at Settings, got %q", err)
	}
	if _, err := p.ModelBuilder(context.Background(), "nope", "", "", nil); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	if _, err := p.Get(context.Background(), "", ""); err == nil {
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
	build, err := p.ModelBuilder(context.Background(), "", "", "", nil)
	if err != nil {
		t.Fatalf("ModelBuilder: %v", err)
	}
	if build("manager", "manager") == nil {
		t.Fatal("nil manager model")
	}
	if _, err := p.Get(context.Background(), "", ""); err != nil {
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
	build, err := p.ModelBuilder(context.Background(), "", "", "",
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
	if got[0].PromptTokens <= 0 || got[0].TotalTokens <= 0 {
		t.Fatalf("the scripted provider must still report usage: %+v", got[0])
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

	// turn 3 answers once the wait reports every worker has finished, quoting
	// what they reported
	convo = append(convo, second,
		schema.ToolMessage(`{"agents":[{"agent_id":"researcher-1","status":"done","result":"researcher done"},`+
			`{"agent_id":"reviewer-2","status":"done","result":"reviewer done"}],"timed_out":false}`, "mock-wait-2"))
	third, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.ToolCalls) != 0 {
		t.Fatalf("the final turn must not call tools: %+v", third.ToolCalls)
	}
	for _, want := range []string{"summarize the inputs", "researcher done", "reviewer done", "## Result", "```chart"} {
		if !strings.Contains(third.Content, want) {
			t.Fatalf("final answer missing %q:\n%s", want, third.Content)
		}
	}
}

func TestMockAnswerChartsReportLengths(t *testing.T) {
	got := mockAnswer("the task", []string{"aa", "bbbb"})
	if !strings.Contains(got, "```chart") {
		t.Fatal("two reports must produce a chart fence")
	}
	if !strings.Contains(got, `"n":2`) || !strings.Contains(got, `"n":4`) {
		t.Fatalf("chart must use report lengths:\n%s", got)
	}
	for _, leak := range []string{"revenue", "sales", "month"} {
		if strings.Contains(strings.ToLower(got), leak) {
			t.Fatalf("offline chart leaked a sample domain %q:\n%s", leak, got)
		}
	}
}

func TestMockAnswerSkipsAChartForOneReport(t *testing.T) {
	got := mockAnswer("the task", []string{"only"})
	if strings.Contains(got, "```chart") {
		t.Fatal("one report is not a comparison")
	}
}

func TestMockChartBlockIsEmptyForAShortSeries(t *testing.T) {
	if mockChartBlock(nil) != "" || mockChartBlock([]string{"x"}) != "" {
		t.Fatal("a short series must not emit a fence")
	}
}

func TestMockManagerContinuesWithoutRespawning(t *testing.T) {
	// A continued run builds a new model whose turn counter is 1 again. If
	// that first call spawned, every extension would fan out forever.
	m := newMockModel("manager")
	ctx := context.Background()
	out, err := m.Generate(ctx, []*schema.Message{
		schema.UserMessage("look into this"),
		schema.ToolMessage(`{"agent_id":"researcher-1"}`, "mock-spawn-1"),
		schema.ToolMessage(`{"agent_id":"reviewer-2"}`, "mock-spawn-2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("a continued run must wait, not spawn: %+v", out.ToolCalls)
	}
	for _, leak := range []string{"spawn_agent"} {
		if strings.Contains(out.Content, leak) {
			t.Fatalf("spawned on a continued run: %s", out.Content)
		}
	}
}

func TestMockWaitCallIDsStayUniqueAfterAContinuedRun(t *testing.T) {
	// Each RunWith is a new model whose turn counter is 1. Reusing
	// mock-wait-1 would hide a later wait_agents result behind the first.
	firstID := nextWaitCallID([]*schema.Message{
		schema.UserMessage("look into this"),
		schema.ToolMessage(`{"agent_id":"researcher-1"}`, "mock-spawn-1"),
	})
	secondID := nextWaitCallID([]*schema.Message{
		schema.UserMessage("look into this"),
		schema.ToolMessage(`{"agent_id":"researcher-1"}`, "mock-spawn-1"),
		&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
			call(firstID, "wait_agents", "{}"),
		}},
		schema.ToolMessage(`{"agents":[{"status":"running"}]}`, firstID),
	})
	if firstID == "" || firstID == secondID {
		t.Fatalf("continued waits must not reuse %q / %q", firstID, secondID)
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

// The reviewer role is what makes --mock and the end-to-end tests exercise the
// memory path at all. Its writes must be derived from the conversation: text
// baked in here would show up in screenshots and expectations as if the
// product had chosen it.
func TestMockReviewerCuratesMemoryFromTheConversation(t *testing.T) {
	m := newMockModel("memory-reviewer")
	ctx := context.Background()
	convo := []*schema.Message{schema.UserMessage("Conversation to review:\n\nuser: collect the inputs")}

	first, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 2 {
		t.Fatalf("the reviewer should write a note and a skill: %+v", first.ToolCalls)
	}
	names := map[string]string{}
	for _, c := range first.ToolCalls {
		names[c.Function.Name] = c.Function.Arguments
	}
	note, ok := names["memory"]
	if !ok {
		t.Fatalf("no memory call: %v", names)
	}
	if !strings.Contains(note, "collect the inputs") {
		t.Fatalf("the note must come from the conversation: %s", note)
	}
	skill, ok := names["skill_manage"]
	if !ok {
		t.Fatalf("no skill_manage call: %v", names)
	}
	if !strings.Contains(skill, `"action":"create"`) || !strings.Contains(skill, "recorded-") ||
		!strings.Contains(skill, "After conversation") {
		t.Fatalf("the skill must be derived from this conversation, not a baked-in procedure: %s", skill)
	}
	if strings.Contains(skill, "collect the inputs") {
		t.Fatal("the skill must not restate the request; that belongs in the note")
	}

	second, err := m.Generate(ctx, append(convo, first, schema.ToolMessage(`{"success":true}`, "x")))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ToolCalls) != 0 || second.Content == "" {
		t.Fatalf("the reviewer must finish with a line, not another tool call: %+v", second)
	}
}

func TestMockCatalogTidyMergesAListedFamilyAndLeavesATidyCatalogAlone(t *testing.T) {
	m := newMockModel("memory-reviewer")
	ctx := context.Background()
	family := []*schema.Message{
		schema.SystemMessage("Curate the catalog."),
		schema.UserMessage("Catalog to curate:\n\nSkills already recorded in this project:\n\n- weekly-rollup-notes — when filing notes\n- weekly-rollup-send — when sending\n\nThese recorded skills share a subject and must become one skill. Merge each group so a later conversation is not handed competing procedures:\n- weekly-rollup-notes, weekly-rollup-send\n"),
	}
	first, err := m.Generate(ctx, family)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "skill_manage" {
		t.Fatalf("a listed family must be merged: %+v", first.ToolCalls)
	}
	if !strings.Contains(first.ToolCalls[0].Function.Arguments, `"action":"merge"`) {
		t.Fatalf("merge missing: %s", first.ToolCalls[0].Function.Arguments)
	}
	if strings.Contains(first.ToolCalls[0].Function.Arguments, `"action":"create"`) ||
		strings.Contains(first.ToolCalls[0].Function.Name, "memory") {
		t.Fatal("a catalog tidy must not store a conversation clip")
	}

	tidy := newMockModel("memory-reviewer")
	alone, err := tidy.Generate(ctx, []*schema.Message{
		schema.SystemMessage("Curate the catalog."),
		schema.UserMessage("Catalog to curate:\n\nSkills already recorded in this project:\n\n- weekly-rollup — when filing the week\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(alone.ToolCalls) != 0 || !strings.Contains(alone.Content, "already curated") {
		t.Fatalf("a tidy catalog must not be rewritten: %+v", alone)
	}

	// eino ChatModelAgent streams. The marker can sit after an earlier user
	// turn the runner injects; first-user-only matching would then spend a
	// whole TidyIterations cap pacing tokens.
	later := []*schema.Message{
		schema.UserMessage("Conversation to review:\n\nhuman: hi\n"),
		schema.UserMessage("Catalog to curate:\n\n- alpha-prep — when preparing\n"),
	}
	if !isCatalogTidy(later) {
		t.Fatal("a catalog marker on a later user turn must still select the tidy script")
	}
}

func TestMockCatalogTidyStreamIsInstant(t *testing.T) {
	m := newMockModel("memory-reviewer")
	start := time.Now()
	sr, err := m.Stream(context.Background(), []*schema.Message{
		schema.UserMessage("Catalog to curate:\n\n- alpha-prep — when preparing\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.ConcatMessageStream(sr); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("catalog tidy streaming must not pace chunks, took %s", d)
	}
}

func TestMockTitleNamerNamesTheConversation(t *testing.T) {
	m := newMockModel("title-namer")
	out, err := m.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("Name this conversation."),
		schema.UserMessage("User: look into the reporting pipeline\n\nAssistant: here is what I found"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content == "" || strings.Contains(out.Content, "look into the reporting pipeline") {
		t.Fatalf("the mock title must be a short label, not the request: %q", out.Content)
	}
	if !strings.Contains(out.Content, "look into") {
		t.Fatalf("the mock title must still come from the request: %q", out.Content)
	}
	if len(out.ToolCalls) != 0 {
		t.Fatalf("the namer is not a worker: %+v", out.ToolCalls)
	}
}

func TestMockCompactSummarizerStaysDerived(t *testing.T) {
	m := newMockModel("compact-summarizer")
	out, err := m.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("Write a briefing."),
		schema.UserMessage("Human: look into the reporting pipeline\n\nAssistant: here is what I found"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Content, "Prior work:") {
		t.Fatalf("mock briefing=%q", out.Content)
	}
	if len(out.ToolCalls) != 0 {
		t.Fatalf("the summarizer is not a worker: %+v", out.ToolCalls)
	}
	if mockBriefing("   ") != "Prior conversation, folded." {
		t.Fatalf("blank=%q", mockBriefing("   "))
	}
}

func TestMockBriefingStaysShorterThanTheSource(t *testing.T) {
	src := "Human: keep going\n\nAssistant: still unfinished"
	got := mockBriefing(src)
	if got == "" {
		t.Fatal("a usable source must still produce a briefing")
	}
	if utf8.RuneCountInString(got) >= utf8.RuneCountInString(src) {
		t.Fatalf("mock briefing %q is not shorter than %q", got, src)
	}
	if strings.HasPrefix(got, "Tool:") || strings.HasPrefix(got, "Human:") || strings.HasPrefix(got, "Assistant:") {
		t.Fatalf("mock briefing looked like a transcript: %q", got)
	}
	tiny := "keep going"
	got = mockBriefing(tiny)
	if utf8.RuneCountInString(got) >= utf8.RuneCountInString(tiny) {
		t.Fatalf("tiny source briefing %q is not shorter than %q", got, tiny)
	}
}

func TestMockBriefingStreamDoesNotPaceLikeTheManager(t *testing.T) {
	for _, role := range []string{"compact-summarizer", "session-memory"} {
		m := newMockModel(role)
		start := time.Now()
		sr, err := m.Stream(context.Background(), []*schema.Message{
			schema.UserMessage(strings.Repeat("word ", 40)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := schema.ConcatMessageStream(sr); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 200*time.Millisecond {
			t.Fatalf("%s briefing stream was paced like a chat: %s", role, time.Since(start))
		}
	}
}

func TestMockTitleFallsBackWhenTheRequestIsBlank(t *testing.T) {
	if got := mockTitle("   "); got != "Conversation" {
		t.Fatalf("blank=%q", got)
	}
	long := strings.Repeat("字", mockTitleMaxRunes+8)
	if got := mockTitle(long); got != strings.Repeat("字", mockTitleMaxRunes) {
		t.Fatalf("clipped=%q", got)
	}
}

func TestTitleRequestReadsABareUserMessage(t *testing.T) {
	got := titleRequest([]*schema.Message{schema.UserMessage("just a question")})
	if got != "just a question" {
		t.Fatalf("got %q", got)
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

func TestMockFailureStopsGenerateAndStream(t *testing.T) {
	SetMockFailure(errors.New("chat model refused the request"))
	t.Cleanup(func() { SetMockFailure(nil) })
	m := newMockModel("manager")
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("Generate must surface the injected failure")
	}
	if _, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("Stream must surface the injected failure")
	}
}

func TestMockFailTimesThenSucceeds(t *testing.T) {
	SetMockFailTimes(1, errors.New("unterminated string"))
	t.Cleanup(func() { SetMockFailure(nil) })
	m := newMockModel("manager")
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("the first call must fail")
	}
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err != nil {
		t.Fatalf("the retry must go through: %v", err)
	}
	SetMockFailTimes(0, errors.New("unterminated string"))
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err != nil {
		t.Fatalf("zero remaining failures must not fail: %v", err)
	}
}

func TestMockFailTimesDoesNotConsumeTheTitleNamer(t *testing.T) {
	SetMockFailTimes(1, errors.New("unterminated string"))
	t.Cleanup(func() { SetMockFailure(nil) })
	namer := newMockModel("title-namer")
	if _, err := namer.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("Name this conversation."),
		schema.UserMessage("User: look into the reporting pipeline"),
	}); err != nil {
		t.Fatalf("the namer must not eat a counted injection: %v", err)
	}
	mgr := newMockModel("manager")
	if _, err := mgr.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("the counted failure still belongs to the manager")
	}
}

// ---------- telemetry ----------

type fakeModel struct {
	out    *schema.Message
	chunks []*schema.Message
	err    error
	// gotOpts is the number of call options the last Generate/Stream received,
	// so a test can prove the reasoning-effort option was (or was not) added.
	gotOpts int
}

func (f *fakeModel) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.gotOpts = len(opts)
	return f.out, f.err
}

func (f *fakeModel) Stream(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.gotOpts = len(opts)
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
	m := wrap(inner, "worker-1", config.Provider{ID: "p", Model: "m"}, "", s.rec())

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

func TestTelemetryRecordsEndpointUsage(t *testing.T) {
	s := &sink{}
	inner := &fakeModel{out: &schema.Message{
		Role:    schema.Assistant,
		Content: "hello",
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens:            12,
			CompletionTokens:        3,
			TotalTokens:             15,
			PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 4},
			CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 2},
		}},
	}}
	m := wrap(inner, "manager", config.Provider{ID: "p", Model: "m"}, "", s.rec())
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}); err != nil {
		t.Fatal(err)
	}
	r := s.all()[0]
	if r.PromptTokens != 12 || r.CompletionTokens != 3 || r.TotalTokens != 15 {
		t.Fatalf("usage not recorded: %+v", r)
	}
	if r.CachedTokens != 4 || r.ReasoningTokens != 2 {
		t.Fatalf("usage details dropped: %+v", r)
	}
}

func TestTelemetryRecordsStreamedUsageFromTheLastChunk(t *testing.T) {
	s := &sink{}
	inner := &fakeModel{chunks: []*schema.Message{
		{Role: schema.Assistant, Content: "one "},
		{Role: schema.Assistant, Content: "two"},
		{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens: 9, CompletionTokens: 2, TotalTokens: 11,
		}}},
	}}
	m := wrap(inner, "manager", config.Provider{ID: "p", Model: "m"}, "", s.rec())
	stream, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("go")})
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	stream.Close()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	recs := s.all()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if recs[0].PromptTokens != 9 || recs[0].CompletionTokens != 2 || recs[0].TotalTokens != 11 {
		t.Fatalf("streamed usage not recorded: %+v", recs[0])
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
	m := wrap(inner, "manager", config.Provider{ID: "p", Model: "m"}, "", s.rec())

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
	m := wrap(&fakeModel{chunks: chunks}, "manager", config.Provider{ID: "p"}, "", s.rec())

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
	m := wrap(&fakeModel{err: boom}, "manager", config.Provider{ID: "p"}, "", s.rec())

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

// With no recorder and no thinking level there must be no wrapper at all, so
// an unwatched, default-effort run pays nothing for telemetry.
func TestWrapWithoutRecorderIsIdentity(t *testing.T) {
	inner := &fakeModel{out: schema.AssistantMessage("x", nil)}
	if got := wrap(inner, "a", config.Provider{}, "", nil); got != model.BaseChatModel(inner) {
		t.Fatal("wrap should return the model untouched when nobody is recording")
	}
}

// A set thinking level must reach the model as one call option. Anything else
// means a conversation set to think hard would silently run on the default.
func TestReasoningEffortIsAppliedPerCall(t *testing.T) {
	inner := &fakeModel{out: schema.AssistantMessage("ok", nil)}
	m := wrap(inner, "worker-1", config.Provider{ID: "p", Model: "m"}, config.ReasoningHigh, nil)
	if _, ok := m.(*recordingModel); !ok {
		t.Fatal("a thinking level must wrap the model even without a recorder")
	}
	if _, err := m.Generate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if inner.gotOpts != 1 {
		t.Fatalf("a set thinking level must add exactly one call option, got %d", inner.gotOpts)
	}
}

// The empty default must send nothing: a non-reasoning endpoint rejects a
// reasoning_effort it never asked for, so an unset level cannot leak one.
func TestNoReasoningEffortSendsNoOption(t *testing.T) {
	s := &sink{}
	inner := &fakeModel{out: schema.AssistantMessage("ok", nil)}
	m := wrap(inner, "worker-1", config.Provider{ID: "p", Model: "m"}, config.ReasoningDefault, s.rec())
	if _, err := m.Generate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if inner.gotOpts != 0 {
		t.Fatalf("an unset thinking level must add no call option, got %d", inner.gotOpts)
	}
}

// A streamed call carries the thinking level too, and a reasoning-only wrapper
// (no recorder) must still relay the stream untouched.
func TestReasoningEffortAppliesToStream(t *testing.T) {
	inner := &fakeModel{chunks: []*schema.Message{{Role: schema.Assistant, Content: "hi"}}}
	m := wrap(inner, "worker-1", config.Provider{ID: "p"}, config.ReasoningMedium, nil)
	if _, ok := m.(*recordingModel); !ok {
		t.Fatal("a thinking level must wrap even without a recorder")
	}
	stream, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("go")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if inner.gotOpts != 1 {
		t.Fatalf("a streamed call must carry the thinking level, got %d opts", inner.gotOpts)
	}
	msg, err := stream.Recv()
	if err != nil || msg.Content != "hi" {
		t.Fatalf("the reasoning-only wrapper corrupted the stream: msg=%v err=%v", msg, err)
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
	multi := []*schema.Message{{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "from the image caption"},
		},
	}}
	if got := lastUserText(multi); got != "from the image caption" {
		t.Fatalf("a multimodal caption must still drive the script: %q", got)
	}
}

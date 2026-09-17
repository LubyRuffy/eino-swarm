package engine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const liveCompactEnv = "ZWAI_LIVE_COMPACT"

var livePlantedFacts = []string{
	"constraint-alpha",
	"decision-bravo",
	"unfinished-charlie",
	"assignment-delta",
	cmpSpawnID,
}

// TestLiveCompactBriefingQualityVsEino scores summarizer *prose* against a
// real endpoint. Default `go test` skips it. The keep-or-delete gate for
// swarm replay stays in TestCompactEffectComparedWithEinoDefault.
func TestLiveCompactBriefingQualityVsEino(t *testing.T) {
	if strings.TrimSpace(os.Getenv(liveCompactEnv)) == "" {
		t.Skip("set " + liveCompactEnv + "=1 to score briefing prose on the configured endpoint")
	}

	cm, modelName := liveCompactModel(t)
	t.Logf("endpoint model %s", modelName)

	fixture := liveCompactFixture()
	system, older, _ := splitCompactable(fixture, 2)
	if len(older) == 0 {
		t.Fatal("fixture had nothing to fold")
	}
	// Same foldable prefix for every arm. The kept ReAct tail is a Finalize
	// problem, not a briefing-prose problem.
	toFold := append(append([]*schema.Message{}, system...), older...)
	source := compactLines(older)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ours, oursIn, err := liveOursBriefing(ctx, cm, older)
	if err != nil {
		t.Fatalf("ours: %v", err)
	}
	einoDef, einoDefIn, err := liveEinoBriefing(ctx, cm, toFold, "")
	if err != nil {
		t.Fatalf("eino default: %v", err)
	}
	einoCustom, einoCustomIn, err := liveEinoBriefing(ctx, cm, toFold, compactPrompt())
	if err != nil {
		t.Fatalf("eino UserInstruction: %v", err)
	}

	arms := []liveArm{
		{name: "ours", briefing: ours, input: oursIn},
		{name: "eino_default", briefing: einoDef, input: einoDefIn},
		{name: "eino_userinstruction", briefing: einoCustom, input: einoCustomIn},
	}
	for _, a := range arms {
		hits := liveFactHits(a.briefing)
		leak := livePromptLeak(source, a.briefing)
		inLeak := !promptTaskAgnostic(a.input)
		t.Logf("\n== %s ==\nrunes=%d fact_hits=%d/%d coding_prompt_in_input=%v output_invents_coding_prompt=%v\n%s\n",
			a.name, utf8.RuneCountInString(a.briefing), hits, len(livePlantedFacts), inLeak, leak, a.briefing)
	}

	verdict, err := liveJudge(ctx, cm, source, arms)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	t.Logf("judge: %s", verdict)
}

type liveArm struct {
	name     string
	briefing string
	input    []*schema.Message
}

func liveCompactFixture() []*schema.Message {
	spawn := schema.ToolCall{
		ID: "c-spawn",
		Function: schema.FunctionCall{
			Name:      spawnAgentToolName,
			Arguments: `{"role":"worker","task":"the first request"}`,
		},
	}
	wait := schema.ToolCall{
		ID: "c-wait",
		Function: schema.FunctionCall{
			Name:      "wait_agents",
			Arguments: `{"agent_ids":["` + cmpSpawnID + `"]}`,
		},
	}
	waitAsst := schema.AssistantMessage("waiting", []schema.ToolCall{wait})
	waitAsst.ResponseMeta = &schema.ResponseMeta{
		Usage: &schema.TokenUsage{PromptTokens: 5000, TotalTokens: 5100},
	}
	return []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("constraint-alpha: never mutate the locked ledger. the first request."),
		schema.AssistantMessage("decision-bravo: keep the existing dispatcher.", []schema.ToolCall{spawn}),
		&schema.Message{Role: schema.Tool, Content: `{"agent_id":"` + cmpSpawnID + `"}`, ToolCallID: "c-spawn"},
		schema.UserMessage("unfinished-charlie: the second collector has not been started. assignment-delta: continue the same assignment."),
		waitAsst,
		&schema.Message{Role: schema.Tool, Content: `{"agents":[{"agent_id":"` + cmpSpawnID + `","status":"running"}]}`, ToolCallID: "c-wait"},
	}
}

func liveCompactModel(t *testing.T) (model.BaseChatModel, string) {
	t.Helper()
	cfg, err := config.Load(config.DefaultDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Configured() {
		t.Skip("configured endpoint missing")
	}
	p, ok := cfg.DefaultProvider()
	if !ok {
		t.Skip("no default provider")
	}
	pid, name := cfg.Swarm.ResolveCompact(p.ID, p.Model)
	pool := provider.New(cfg)
	setup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := pool.ModelBuilder(setup, pid, name, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b(CompactAgentID, CompactAgentID), name
}

func liveOursBriefing(ctx context.Context, cm model.BaseChatModel, older []*schema.Message) (string, []*schema.Message, error) {
	cap := &captureChatModel{inner: cm}
	in := []*schema.Message{
		schema.SystemMessage(compactPrompt()),
		schema.UserMessage(compactInputFromADK("", older)),
	}
	out, err := compactGenerate(ctx, cap, in)
	if err != nil {
		return "", cap.in, err
	}
	return sanitizeCompact(assistantText(out)), cap.in, nil
}

func liveEinoBriefing(ctx context.Context, cm model.BaseChatModel, msgs []*schema.Message, userInst string) (string, []*schema.Message, error) {
	cap := &captureChatModel{inner: cm}
	cfg := &summarization.Config{
		Model:   cap,
		Trigger: &summarization.TriggerCondition{ContextTokens: 10, ContextMessages: 1},
	}
	if userInst != "" {
		cfg.UserInstruction = userInst
	}
	mw, err := summarization.New(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	_, next, err := mw.BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{Messages: cloneMsgs(msgs)}, nil)
	if err != nil {
		return "", cap.in, err
	}
	raw := assistantText(cap.out)
	if next != nil && len(next.Messages) > 0 {
		// DefaultFinalize stuffs the model output into the last user message.
		last := next.Messages[len(next.Messages)-1]
		if last != nil && last.Role == schema.User && strings.TrimSpace(last.Content) != "" {
			raw = last.Content
		}
	}
	return strings.TrimSpace(raw), cap.in, nil
}

func liveFactHits(briefing string) int {
	n := 0
	for _, fact := range livePlantedFacts {
		if strings.Contains(briefing, fact) {
			n++
		}
	}
	return n
}

func livePromptLeak(source, briefing string) bool {
	for _, w := range einoCodingAgentPromptWords {
		if strings.Contains(briefing, w) && !strings.Contains(source, w) {
			return true
		}
	}
	return false
}

func liveJudge(ctx context.Context, cm model.BaseChatModel, source string, arms []liveArm) (string, error) {
	var b strings.Builder
	b.WriteString("Score each briefing for a later model that must continue the work.\n")
	b.WriteString("Use only the source conversation. Do not reward tools, files, or languages that are absent from it.\n\n")
	b.WriteString("Source conversation:\n")
	b.WriteString(source)
	b.WriteString("\n\n")
	for _, a := range arms {
		b.WriteString("### ")
		b.WriteString(a.name)
		b.WriteString("\n")
		b.WriteString(a.briefing)
		b.WriteString("\n\n")
	}
	b.WriteString("Reply with JSON only. The arms object MUST include every name ours, eino_default, eino_userinstruction.\n")
	b.WriteString("{\"arms\":{\"ours\":{\"facts\":0,\"current\":0,\"no_invention\":0,\"density\":0},\"eino_default\":{\"facts\":0,\"current\":0,\"no_invention\":0,\"density\":0},\"eino_userinstruction\":{\"facts\":0,\"current\":0,\"no_invention\":0,\"density\":0}},\"winner\":\"ours|eino_default|eino_userinstruction|tie\",\"why\":\"<one sentence>\"}\n")
	b.WriteString("Each score is 0, 1, or 2. facts covers standing constraints, decisions, unfinished work, and names. current is the latest human ask. no_invention is 2 when nothing extra was invented. density is 2 when the briefing is shorter than the source and still usable.\n")

	out, err := compactGenerate(ctx, cm, []*schema.Message{
		schema.SystemMessage("Return JSON only."),
		schema.UserMessage(b.String()),
	})
	if err != nil {
		return "", err
	}
	text := assistantText(out)
	raw := extractJSONObject(text)
	if raw == "" {
		return text, nil
	}
	var parsed json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return text, nil
	}
	return raw, nil
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func assistantText(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if s := strings.TrimSpace(m.Content); s != "" {
		return s
	}
	return strings.TrimSpace(m.ReasoningContent)
}

type captureChatModel struct {
	inner model.BaseChatModel
	in    []*schema.Message
	out   *schema.Message
}

func (m *captureChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.in = cloneMsgs(in)
	out, err := m.inner.Generate(ctx, in, opts...)
	m.out = out
	return out, err
}

func (m *captureChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, in, opts...)
}

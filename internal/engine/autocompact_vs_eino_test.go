package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// These are words from eino's default summarizer prompt (coding-agent
// compact). They must not appear in ours; if they start showing up, we
// copied the wheel we were trying not to copy.
var einoCodingAgentPromptWords = []string{"Bash", "Grep", "typescript", "<analysis>"}

const (
	cmpCurrentRequest = "the current request"
	cmpSpawnID        = "worker-1"
	cmpBriefing       = "folded briefing"
)

// TestCompactEffectComparedWithEinoDefault is the keep-or-delete gate for
// autoCompact. Briefing prose is scored by TestLiveCompactBriefingQualityVsEino
// (opt-in, real endpoint). This test scores whether a mid-ReAct swarm can
// continue after the rewrite. If ours does not beat eino's DefaultFinalize
// on those checks, the custom middleware is dead weight and should be deleted.
func TestCompactEffectComparedWithEinoDefault(t *testing.T) {
	fixture := swarmCompactFixture()
	beforeTok := promptTokenCount(fixture)

	einoStub := &scriptedChatModel{out: cmpBriefing}
	oursStub := &scriptedChatModel{out: cmpBriefing}
	hybridStub := &scriptedChatModel{out: cmpBriefing}

	einoState, einoErr := rewriteWithEino(t, einoStub, cloneMsgs(fixture), nil)
	oursState, oursErr := rewriteWithOurs(t, oursStub, cloneMsgs(fixture))
	hybridState, hybridErr := rewriteWithEino(t, hybridStub, cloneMsgs(fixture), ourFinalize)

	einoScore := scoreCompact("eino-default", einoState, einoErr, einoStub.in, beforeTok)
	oursScore := scoreCompact("ours", oursState, oursErr, oursStub.in, beforeTok)
	hybridScore := scoreCompact("eino+our-finalize", hybridState, hybridErr, hybridStub.in, beforeTok)

	einoErrScore := scoreEinoError(t, cloneMsgs(fixture))
	oursErrScore := scoreOursError(t, cloneMsgs(fixture))
	einoScore.checks["swallows_summarizer_error"] = einoErrScore
	oursScore.checks["swallows_summarizer_error"] = oursErrScore
	hybridScore.checks["swallows_summarizer_error"] = einoErrScore

	logScorecard(t, einoScore, oursScore, hybridScore)

	if oursScore.swarm() <= einoScore.swarm() {
		t.Fatalf("custom compact is not a swarm-effect lift (ours=%d eino=%d); delete autoCompact and wrap eino",
			oursScore.swarm(), einoScore.swarm())
	}
	for _, k := range []string{
		"keeps_spawn_roster",
		"keeps_inflight_wait",
		"keeps_last_human_as_user",
		"prompt_task_agnostic",
		"swallows_summarizer_error",
	} {
		if !oursScore.checks[k] {
			t.Fatalf("lost compact invariant %s — a later Generate cannot continue this swarm", k)
		}
	}
	if hybridScore.swarm() < oursScore.swarm() && hybridScore.checks["keeps_spawn_roster"] {
		t.Log("eino Generate + our Finalize keeps the roster; the wheel worth keeping is Finalize, not the summarizer call")
	}
}

func TestCompactPromptDoesNotCarryEinoCodingAgentExamples(t *testing.T) {
	prompt := compactPrompt()
	for _, w := range einoCodingAgentPromptWords {
		if strings.Contains(prompt, w) {
			t.Fatalf("compactPrompt leaked eino's coding-agent template (%q)", w)
		}
	}
}

func swarmCompactFixture() []*schema.Message {
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
		schema.UserMessage("the first request"),
		schema.AssistantMessage("", []schema.ToolCall{spawn}),
		&schema.Message{Role: schema.Tool, Content: `{"agent_id":"` + cmpSpawnID + `"}`, ToolCallID: "c-spawn"},
		schema.UserMessage(cmpCurrentRequest),
		waitAsst,
		&schema.Message{Role: schema.Tool, Content: `{"agents":[{"agent_id":"` + cmpSpawnID + `","status":"running"}]}`, ToolCallID: "c-wait"},
	}
}

func ourFinalize(ctx context.Context, original []*schema.Message, summary *schema.Message) ([]*schema.Message, error) {
	text := ""
	if summary != nil {
		text = summary.Content
	}
	system, older, tail := splitCompactable(original, 2)
	roster := pinWorkerPairs([]swarm.RestoredWorker{{ID: cmpSpawnID, Role: "worker"}}, nil)
	out := assembleCompacted(system, sanitizeCompact(text), older, tail, dropPairsAlreadyIn(roster, tail))
	clearBilledUsage(out)
	return out, nil
}

func rewriteWithEino(t *testing.T, stub *scriptedChatModel, msgs []*schema.Message, finalize summarization.FinalizeFunc) (*adk.ChatModelAgentState, error) {
	t.Helper()
	cfg := &summarization.Config{
		Model:   stub,
		Trigger: &summarization.TriggerCondition{ContextTokens: 10},
	}
	if finalize != nil {
		cfg.Finalize = finalize
	}
	mw, err := summarization.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := mw.BeforeModelRewriteState(context.Background(), &adk.ChatModelAgentState{Messages: msgs}, nil)
	return next, err
}

func rewriteWithOurs(t *testing.T, stub *scriptedChatModel, msgs []*schema.Message) (*adk.ChatModelAgentState, error) {
	t.Helper()
	e := newTestEngine(t)
	// This scorecard is the eino fallback. Session-memory copy is tested
	// on its own; a live briefing here would skip the summarizer stub.
	e.sessions.stop()
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: cmpSpawnID, Role: "worker",
	})
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	mw.model = stub
	_, next, err := mw.BeforeModelRewriteState(context.Background(), &adk.ChatModelAgentState{Messages: msgs}, nil)
	return next, err
}

func scoreEinoError(t *testing.T, msgs []*schema.Message) bool {
	t.Helper()
	stub := &scriptedChatModel{err: errors.New("summarizer down")}
	_, err := rewriteWithEino(t, stub, msgs, nil)
	return err == nil
}

func scoreOursError(t *testing.T, msgs []*schema.Message) bool {
	t.Helper()
	stub := &scriptedChatModel{err: errors.New("summarizer down")}
	next, err := rewriteWithOurs(t, stub, msgs)
	if err != nil {
		return false
	}
	return next != nil && len(next.Messages) == len(msgs)
}

type compactScore struct {
	name   string
	checks map[string]bool
}

func (s compactScore) swarm() int {
	n := 0
	for _, k := range []string{
		"keeps_spawn_roster",
		"keeps_inflight_wait",
		"keeps_last_human_as_user",
		"tool_safe",
		"prompt_task_agnostic",
		"swallows_summarizer_error",
	} {
		if s.checks[k] {
			n++
		}
	}
	return n
}

func scoreCompact(name string, state *adk.ChatModelAgentState, err error, summarizerIn []*schema.Message, beforeTok int) compactScore {
	s := compactScore{name: name, checks: map[string]bool{}}
	s.checks["rewrite_ok"] = err == nil && state != nil && len(state.Messages) > 0
	if !s.checks["rewrite_ok"] {
		s.checks["shrinks"] = false
		s.checks["keeps_spawn_roster"] = false
		s.checks["keeps_inflight_wait"] = false
		s.checks["keeps_last_human_as_user"] = false
		s.checks["tool_safe"] = false
		s.checks["keeps_system"] = false
		s.checks["prompt_task_agnostic"] = promptTaskAgnostic(summarizerIn)
		return s
	}
	msgs := state.Messages
	// Roster + tail can keep the same message count; the budget that
	// matters is tokens (eino billed TotalTokens vs our cleared usage).
	s.checks["shrinks"] = promptTokenCount(msgs) < beforeTok
	s.checks["keeps_system"] = len(msgs) > 0 && msgs[0] != nil && msgs[0].Role == schema.System
	s.checks["keeps_spawn_roster"] = hasSpawnID(msgs, cmpSpawnID)
	s.checks["keeps_inflight_wait"] = hasToolCall(msgs, "wait_agents") && hasToolResult(msgs, "c-wait")
	s.checks["keeps_last_human_as_user"] = hasExactUser(msgs, cmpCurrentRequest)
	s.checks["tool_safe"] = tailToolSafe(msgs) || tailToolSafe(dropLeadingSystem(msgs))
	s.checks["prompt_task_agnostic"] = promptTaskAgnostic(summarizerIn)
	return s
}

func promptTaskAgnostic(in []*schema.Message) bool {
	blob := joinedContent(in)
	if blob == "" {
		return false
	}
	for _, w := range einoCodingAgentPromptWords {
		if strings.Contains(blob, w) {
			return false
		}
	}
	return true
}

func logScorecard(t *testing.T, rows ...compactScore) {
	t.Helper()
	keys := []string{
		"rewrite_ok", "shrinks", "keeps_system",
		"keeps_spawn_roster", "keeps_inflight_wait", "keeps_last_human_as_user",
		"tool_safe", "prompt_task_agnostic", "swallows_summarizer_error",
	}
	var b strings.Builder
	b.WriteString("compact effect scorecard (structure, not prose):\n")
	b.WriteString("check                      ")
	for _, r := range rows {
		b.WriteString("  ")
		b.WriteString(pad(r.name, 18))
	}
	b.WriteByte('\n')
	for _, k := range keys {
		b.WriteString(pad(k, 26))
		for _, r := range rows {
			cell := "no"
			if r.checks[k] {
				cell = "yes"
			}
			b.WriteString("  ")
			b.WriteString(pad(cell, 18))
		}
		b.WriteByte('\n')
	}
	b.WriteString(pad("swarm_score", 26))
	for _, r := range rows {
		b.WriteString("  ")
		b.WriteString(pad(strconv.Itoa(r.swarm())+"/6", 18))
	}
	t.Log(b.String())
}

func hasSpawnID(msgs []*schema.Message, id string) bool {
	for _, m := range msgs {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		var r struct {
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal([]byte(m.Content), &r) == nil && r.AgentID == id {
			return true
		}
	}
	return false
}

func hasToolCall(msgs []*schema.Message, name string) bool {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == name {
				return true
			}
		}
	}
	return false
}

func hasToolResult(msgs []*schema.Message, callID string) bool {
	for _, m := range msgs {
		if m != nil && m.Role == schema.Tool && m.ToolCallID == callID {
			return true
		}
	}
	return false
}

func hasExactUser(msgs []*schema.Message, text string) bool {
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && m.Content == text {
			return true
		}
	}
	return false
}

func dropLeadingSystem(msgs []*schema.Message) []*schema.Message {
	i := 0
	for i < len(msgs) && msgs[i] != nil && msgs[i].Role == schema.System {
		i++
	}
	return msgs[i:]
}

func joinedContent(msgs []*schema.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		b.WriteString(m.Content)
		b.WriteByte('\n')
		for _, tc := range m.ToolCalls {
			b.WriteString(tc.Function.Name)
			b.WriteByte(' ')
			b.WriteString(tc.Function.Arguments)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func cloneMsgs(in []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, len(in))
	for i, m := range in {
		if m == nil {
			continue
		}
		c := *m
		if m.ToolCalls != nil {
			c.ToolCalls = append([]schema.ToolCall(nil), m.ToolCalls...)
		}
		if m.ResponseMeta != nil {
			rm := *m.ResponseMeta
			if m.ResponseMeta.Usage != nil {
				u := *m.ResponseMeta.Usage
				rm.Usage = &u
			}
			c.ResponseMeta = &rm
		}
		out[i] = &c
	}
	return out
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

type scriptedChatModel struct {
	mu   sync.Mutex
	in   []*schema.Message
	out  string
	err  error
	hang bool
}

func (m *scriptedChatModel) Generate(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.in = cloneMsgs(in)
	err := m.err
	out := m.out
	hang := m.hang
	m.mu.Unlock()
	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(out, nil), nil
}

func (m *scriptedChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream unused")
}

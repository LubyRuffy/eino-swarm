package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// ToolAskUser is the manager-only tool that pauses a turn for a structured
// question. The name travels in transcripts and Trace, so renaming it is a
// protocol change.
const ToolAskUser = "ask_user"

// ErrAskMismatch means the conversation is running but not waiting for that
// question. Distinct from ErrIdle so the UI can keep the card up.
var ErrAskMismatch = errors.New("engine: that question is not waiting")

const (
	askMinQuestions = 1
	askMaxQuestions = 3
	askMinOptions   = 2
	askMaxOptions   = 4
	// Host-injected free-form option. Models must not send this id.
	askOtherID    = "other"
	askOtherLabel = "Other"
)

// AskOption is one mutually exclusive choice on a question.
type AskOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskQuestion is one prompt in an ask_user call.
type AskQuestion struct {
	ID      string      `json:"id"`
	Header  string      `json:"header,omitempty"`
	Prompt  string      `json:"prompt"`
	Options []AskOption `json:"options"`
}

// AskAnswer is the tool-result payload for one question id.
type AskAnswer struct {
	Answers []string `json:"answers"`
}

// AskAnswers maps question id → selected labels or free text.
type AskAnswers map[string]AskAnswer

type askReply struct {
	answers AskAnswers
	err     error
}

type pendingAsk struct {
	callID    string
	questions []AskQuestion
	ch        chan askReply
}

// ParseAskArguments reads the model's ask_user JSON. Invalid input is a
// tool-level failure, not a Go error that would kill the ReAct graph.
func ParseAskArguments(args string) ([]AskQuestion, error) {
	var raw struct {
		Questions []AskQuestion `json:"questions"`
	}
	s := strings.TrimSpace(args)
	if s == "" || s == "{}" {
		return nil, fmt.Errorf("ask_user: questions are required")
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("ask_user: could not read the arguments: %v", err)
	}
	return NormalizeAskQuestions(raw.Questions)
}

// NormalizeAskQuestions validates the schema and injects the host Other
// option. Sample tasks do not belong here: ids and labels come from the model.
func NormalizeAskQuestions(in []AskQuestion) ([]AskQuestion, error) {
	if len(in) < askMinQuestions || len(in) > askMaxQuestions {
		return nil, fmt.Errorf("ask_user: need %d to %d questions", askMinQuestions, askMaxQuestions)
	}
	seenQ := map[string]bool{}
	out := make([]AskQuestion, 0, len(in))
	for _, q := range in {
		id := askIdent(q.ID)
		if id == "" {
			return nil, fmt.Errorf("ask_user: each question needs an id")
		}
		if seenQ[id] {
			return nil, fmt.Errorf("ask_user: duplicate question id %q", id)
		}
		seenQ[id] = true
		prompt := strings.TrimSpace(q.Prompt)
		if prompt == "" {
			return nil, fmt.Errorf("ask_user: each question needs a prompt")
		}
		opts, err := normalizeAskOptions(q.Options)
		if err != nil {
			return nil, err
		}
		out = append(out, AskQuestion{
			ID:      id,
			Header:  clip(strings.TrimSpace(q.Header), 32),
			Prompt:  prompt,
			Options: opts,
		})
	}
	return out, nil
}

func normalizeAskOptions(in []AskOption) ([]AskOption, error) {
	if len(in) < askMinOptions || len(in) > askMaxOptions {
		return nil, fmt.Errorf("ask_user: each question needs %d to %d options", askMinOptions, askMaxOptions)
	}
	seen := map[string]bool{}
	out := make([]AskOption, 0, len(in)+1)
	for _, o := range in {
		id := askIdent(o.ID)
		if id == "" {
			id = askIdent(o.Label)
		}
		if id == "" {
			return nil, fmt.Errorf("ask_user: each option needs an id")
		}
		if id == askOtherID {
			return nil, fmt.Errorf("ask_user: do not include a free-form option; the host adds it")
		}
		if seen[id] {
			return nil, fmt.Errorf("ask_user: duplicate option id %q", id)
		}
		seen[id] = true
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return nil, fmt.Errorf("ask_user: each option needs a label")
		}
		out = append(out, AskOption{
			ID:          id,
			Label:       label,
			Description: strings.TrimSpace(o.Description),
		})
	}
	out = append(out, AskOption{ID: askOtherID, Label: askOtherLabel})
	return out, nil
}

func askIdent(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else if r == '-' || unicode.IsSpace(r) {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// FormatAskResult is the JSON the model reads after the human answers.
func FormatAskResult(answers AskAnswers) string {
	if answers == nil {
		answers = AskAnswers{}
	}
	body, _ := json.Marshal(struct {
		Answers AskAnswers `json:"answers"`
	}{Answers: answers})
	return string(body)
}

// FreeTextAnswers treats one typed line as the Other choice for every
// pending question. Composer Enter while a card is up must not queue a
// follow-up that can never start.
func FreeTextAnswers(questions []AskQuestion, text string) AskAnswers {
	text = strings.TrimSpace(text)
	out := AskAnswers{}
	for _, q := range questions {
		out[q.ID] = AskAnswer{Answers: []string{text}}
	}
	return out
}

func resolveAskAnswers(questions []AskQuestion, raw AskAnswers) (AskAnswers, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("ask_user: no questions are waiting")
	}
	out := AskAnswers{}
	for _, q := range questions {
		got, ok := raw[q.ID]
		if !ok || len(got.Answers) == 0 || strings.TrimSpace(got.Answers[0]) == "" {
			return nil, fmt.Errorf("ask_user: missing an answer for %q", q.ID)
		}
		ans := strings.TrimSpace(got.Answers[0])
		if !askAnswerAllowed(q, ans) {
			return nil, fmt.Errorf("ask_user: %q is not a choice on %q", ans, q.ID)
		}
		out[q.ID] = AskAnswer{Answers: []string{ans}}
	}
	return out, nil
}

func askAnswerAllowed(q AskQuestion, ans string) bool {
	for _, o := range q.Options {
		if o.ID == askOtherID {
			return true
		}
		if ans == o.Label || ans == o.ID {
			return true
		}
	}
	return false
}

func splitToolCallText(raw string) (name, args string) {
	raw = strings.TrimSpace(raw)
	open := strings.Index(raw, "(")
	if open < 0 {
		return raw, ""
	}
	name = strings.TrimSpace(raw[:open])
	args = raw[open+1:]
	if strings.HasSuffix(args, ")") {
		args = args[:len(args)-1]
	}
	return name, args
}

func isAskUserToolText(raw string) bool {
	name, _ := splitToolCallText(raw)
	return name == ToolAskUser
}

func (rt *runtime) awaitingAnswer() bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.ask != nil
}

func (rt *runtime) currentTurnID() string {
	if rt == nil {
		return ""
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.turnID
}

// waitAsk pauses the in-flight tool until the human answers, interrupts, or
// the process dies. The channel is armed before we look up the call id so a
// client that already saw tool_call cannot land on AnswerTurn with nobody
// listening. A reply that arrived first is taken from the stash.
func (rt *runtime) waitAsk(ctx context.Context, questions []AskQuestion) (AskAnswers, error) {
	ch := make(chan askReply, 1)
	callID := rt.peekAskCallID()
	rt.mu.Lock()
	if rt.askStash != nil && (rt.askStashCallID == "" || callID == "" || rt.askStashCallID == callID) {
		reply := *rt.askStash
		rt.askStash = nil
		rt.askStashCallID = ""
		rt.mu.Unlock()
		return reply.answers, reply.err
	}
	rt.ask = &pendingAsk{callID: callID, questions: questions, ch: ch}
	rt.mu.Unlock()
	if callID == "" {
		if id := rt.peekAskCallID(); id != "" {
			rt.mu.Lock()
			if rt.ask != nil {
				rt.ask.callID = id
			}
			rt.mu.Unlock()
		}
	}
	defer func() {
		done := rt.peekAskCallID()
		rt.mu.Lock()
		if rt.ask != nil && rt.ask.callID != "" {
			done = rt.ask.callID
		}
		if done == "" {
			done = callID
		}
		rt.ask = nil
		rt.askCompletedCallID = done
		rt.mu.Unlock()
	}()

	select {
	case reply := <-ch:
		return reply.answers, reply.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (rt *runtime) peekAskCallID() string {
	if rt == nil {
		return ""
	}
	return rt.askCallIDOnTurn(rt.currentTurnID())
}

func (rt *runtime) signalAsk(callID string, reply askReply) error {
	for {
		rt.mu.Lock()
		if rt.ask != nil && rt.ask.ch != nil {
			expected := rt.ask.callID
			ch := rt.ask.ch
			rt.mu.Unlock()
			if expected != "" && callID != "" && expected != callID {
				return ErrAskMismatch
			}
			select {
			case ch <- reply:
				return nil
			default:
				return ErrAskMismatch
			}
		}
		if !rt.running {
			rt.mu.Unlock()
			return ErrIdle
		}
		finished := rt.askCompletedCallID
		tid := rt.turnID
		rt.mu.Unlock()

		openID := rt.askCallIDOnTurn(tid)
		if finished != "" && (openID == "" || openID == finished) {
			return ErrAskMismatch
		}

		rt.mu.Lock()
		if rt.ask != nil && rt.ask.ch != nil {
			rt.mu.Unlock()
			continue
		}
		if !rt.running {
			rt.mu.Unlock()
			return ErrIdle
		}
		// tool_call is recorded before InvokableRun arms the channel.
		rt.askStash = &reply
		rt.askStashCallID = callID
		rt.mu.Unlock()
		return nil
	}
}

func (rt *runtime) askCallIDOnTurn(tid string) string {
	if rt == nil || rt.engine == nil || tid == "" {
		return ""
	}
	events, err := rt.engine.store.ListTurnEvents(tid)
	if err != nil {
		return ""
	}
	open := orphanedAskCall(events)
	if open == nil {
		return ""
	}
	return open.ToolCallID
}

// AnswerTurn delivers structured choices for the in-flight ask_user call.
func (e *Engine) AnswerTurn(threadID, callID string, raw AskAnswers) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	if !rt.status().Running {
		return ErrIdle
	}
	questions := rt.pendingAskQuestions(callID)
	if questions == nil {
		questions = rt.questionsFromOpenCall(callID)
	}
	if questions == nil {
		return ErrAskMismatch
	}
	answers, err := resolveAskAnswers(questions, raw)
	if err != nil {
		return err
	}
	return rt.signalAsk(callID, askReply{answers: answers})
}

// AnswerTurnText answers every pending question with the typed line as Other.
func (e *Engine) AnswerTurnText(threadID, text string) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	if !rt.status().Running {
		return ErrIdle
	}
	questions := rt.pendingAskQuestions("")
	if questions == nil {
		questions = rt.questionsFromOpenCall("")
	}
	if questions == nil {
		return ErrAskMismatch
	}
	return rt.signalAsk("", askReply{answers: FreeTextAnswers(questions, text)})
}

func (rt *runtime) questionsFromOpenCall(callID string) []AskQuestion {
	if rt == nil || rt.engine == nil {
		return nil
	}
	tid := rt.currentTurnID()
	if tid == "" {
		return nil
	}
	events, err := rt.engine.store.ListTurnEvents(tid)
	if err != nil {
		return nil
	}
	open := orphanedAskCall(events)
	if open == nil {
		return nil
	}
	if callID != "" && open.ToolCallID != "" && open.ToolCallID != callID {
		return nil
	}
	_, args := splitToolCallText(open.Text)
	qs, err := ParseAskArguments(args)
	if err != nil {
		return nil
	}
	return qs
}

func (rt *runtime) pendingAskQuestions(callID string) []AskQuestion {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.ask == nil {
		return nil
	}
	if callID == "" {
		return rt.ask.questions
	}
	expected := rt.ask.callID
	if expected == "" {
		// waitAsk armed before the tool_call row was visible; do not
		// accept a random id — AnswerTurn will match against the event.
		return nil
	}
	if expected != callID {
		return nil
	}
	return rt.ask.questions
}

func orphanedAskCall(events []store.Event) *orphanedToolCall {
	for _, c := range orphanedToolCalls(events) {
		if isAskUserToolText(c.Text) {
			got := c
			return &got
		}
	}
	return nil
}

func (rt *runtime) resolveOrphanedAsk(ctx context.Context, turn *store.Turn, messages []adk.Message) []adk.Message {
	if rt == nil || turn == nil {
		return messages
	}
	events, err := rt.engine.store.ListTurnEvents(turn.ID)
	if err != nil {
		return messages
	}
	open := orphanedAskCall(events)
	if open == nil {
		return messages
	}
	_, args := splitToolCallText(open.Text)
	questions, perr := ParseAskArguments(args)
	if perr != nil {
		rt.engine.record(store.Event{
			ThreadID: turn.ThreadID, TurnID: turn.ID,
			Kind: swarm.NotifyToolResult.String(), AgentID: open.AgentID, Role: open.Role,
			ToolCallID: open.ToolCallID, Text: resumeToolStopped, Err: resumeToolStopped,
		})
		return messages
	}
	waitCtx := ctx
	if rt.reg != nil {
		var stop func()
		waitCtx, stop = rt.reg.WithEpoch(ctx)
		defer stop()
	}
	answers, werr := rt.waitAsk(waitCtx, questions)
	body := FormatAskResult(answers)
	if werr != nil {
		raw, _ := json.Marshal(map[string]any{"ok": false, "error": werr.Error()})
		body = string(raw)
	}
	agent := open.AgentID
	if agent == "" {
		agent = swarm.DefaultManagerID
	}
	rt.engine.record(store.Event{
		ThreadID: turn.ThreadID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), AgentID: agent, Role: open.Role,
		ToolCallID: open.ToolCallID, Text: body,
	})
	name, args := splitToolCallText(open.Text)
	if !hasAssistantToolCall(messages, open.ToolCallID) {
		messages = append(messages, schema.AssistantMessage("", []schema.ToolCall{{
			ID:       open.ToolCallID,
			Function: schema.FunctionCall{Name: name, Arguments: args},
		}}))
	}
	return stitchManagerToolResults(messages, []store.Event{{
		ToolCallID: open.ToolCallID, Text: body,
	}})
}

func hasAssistantToolCall(msgs []adk.Message, callID string) bool {
	id := strings.TrimSpace(callID)
	if id == "" {
		return false
	}
	for _, m := range msgs {
		if m == nil || m.Role != schema.Assistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if strings.TrimSpace(tc.ID) == id {
				return true
			}
		}
	}
	return false
}

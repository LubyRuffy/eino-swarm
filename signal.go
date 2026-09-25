// signal.go — the frontend-facing notification contract: event kinds, the UI
// plugin interface, and the mapping from eino agent events to notifications.
// The run loops themselves live in run.go.
package swarm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Callback is the lightweight UI form: a plain function receiving events.
// It is adapted to UI automatically when passed to Run.
type Callback func(Notification)

// UI is the plugin interface for frontends (bubbletea TUI, web UI, logger…).
// Everything the swarm knows about a frontend is this interface — the core
// never changes for a new frontend; implement UI and hand it to Run.
//
// Implementation contract:
//   - OnNotify is called from the swarm's event pump goroutine(s); heavy work
//     or rendering should be forwarded to the UI's own goroutine.
//   - Streaming semantics: each NotifyDelta/NotifyReasoningDelta/NotifyToolDelta
//     carries the FULL accumulated text so far ("a", "ab", "abc"…), so a UI
//     renders by overwriting the target pane — no buffering needed. Answer
//     and reasoning reset at every turn boundary (NotifyTurn). Tool deltas
//     accumulate per ToolCallID until NotifyToolResult.
type UI interface {
	// OnNotify receives one event. Called from the swarm's event pump
	// goroutine(s); implementations must not block for long.
	OnNotify(n Notification)
	// OnDone is called exactly once when the run finishes (final = manager
	// answer, err = failure).
	OnDone(final string, err error)
}

// cbUI adapts a plain function to the UI interface.
type cbUI struct{ fn Callback }

func (c cbUI) OnNotify(n Notification) { c.fn(n) }
func (c cbUI) OnDone(final string, err error) {
	if c.fn == nil {
		return
	}
	c.fn(doneNotif(final, err))
}

// NotifyKind enumerates the events delivered to the UI.
type NotifyKind int

const (
	NotifyAgentMessage   NotifyKind = iota // an assistant message completed
	NotifySpawned                          // a worker was spawned (Text=system prompt, Role=role, AgentID set)
	NotifyFinished                         // a worker finished (Text=result; Err set on failure)
	NotifyToolCall                         // a tool call was issued (Text="name(args)")
	NotifyToolResult                       // a tool returned (Text=clipped result, newlines kept)
	NotifyTurn                             // an agent started a new model turn (Text="turn N")
	NotifyDelta                            // streamed answer text, accumulated within the turn
	NotifyReasoningDelta                   // streamed reasoning, accumulated within the turn
	NotifyToolDelta                        // streamed tool output, accumulated within the call
	NotifyToolCallDelta                    // streamed tool-call arguments, before the call is issued
	NotifyDone                             // manager final answer; run complete
	NotifyError                            // fatal error; run failed
)

var notifyNames = map[NotifyKind]string{
	NotifyAgentMessage:   "agent_message",
	NotifySpawned:        "spawned",
	NotifyFinished:       "finished",
	NotifyToolCall:       "tool_call",
	NotifyToolResult:     "tool_result",
	NotifyTurn:           "turn",
	NotifyDelta:          "delta",
	NotifyReasoningDelta: "reasoning_delta",
	NotifyToolDelta:      "tool_delta",
	NotifyToolCallDelta:  "tool_call_delta",
	NotifyDone:           "done",
	NotifyError:          "error",
}

func (k NotifyKind) String() string {
	if s, ok := notifyNames[k]; ok {
		return s
	}
	return "unknown"
}

// ParseNotifyKind is the inverse of NotifyKind.String; ok is false for an
// unrecognized name. Frontends that persist events by name use it to read
// them back.
func ParseNotifyKind(s string) (NotifyKind, bool) {
	for k, name := range notifyNames {
		if name == s {
			return k, true
		}
	}
	return 0, false
}

// DefaultManagerID is the agent ID of the top-level manager agent created by
// Run. It appears as Notification.AgentID for manager-scope events.
const DefaultManagerID = "manager"

// Notification is one UI event.
//
// Delta semantics: NotifyDelta/NotifyReasoningDelta/NotifyToolDelta carry the
// FULL text accumulated so far ("a", then "ab", then "abc") — a UI overwrites
// the bubble per event, no buffering needed. Reasoning and answer use separate
// accumulators and reset on NotifyTurn. Tool deltas key on ToolCallID and
// reset when that call's NotifyToolResult arrives.
type Notification struct {
	Kind    NotifyKind
	AgentID string // emitting agent (DefaultManagerID for the top level)
	Role    string
	Text    string // accumulated text / tool summary / result
	Err     error  // set for NotifyError and for a failed NotifyFinished

	// ToolCallID pairs NotifyToolCall with its NotifyToolResult. Agents issue
	// several calls in one turn, so matching by position or name is wrong;
	// match on this instead.
	ToolCallID string
}

func doneNotif(final string, err error) Notification {
	if err != nil {
		return Notification{Kind: NotifyError, AgentID: DefaultManagerID, Text: final, Err: err}
	}
	return Notification{Kind: NotifyDone, AgentID: DefaultManagerID, Text: final}
}

// asUI normalizes the accepted UI forms (nil / Callback / func / UI) into one
// internal sink.
func asUI(v any) UI {
	switch u := v.(type) {
	case nil:
		return nil
	case UI:
		return u
	case Callback:
		return cbUI{fn: u}
	case func(Notification):
		return cbUI{fn: Callback(u)}
	default:
		// Unknown type: accept nothing. Callers pass the wrong type only by
		// mistake; a compile-time UI/Callback is expected.
		return nil
	}
}

// ---------- notification sink plumbing ----------

// SetHostNotify is the sink workers use when no Run/RunWith is on the stack.
// Install it before the first RunWith so a /goal session yield cannot mute
// in-flight sub-agents: compact and the next manager turn are often seconds
// to minutes later.
func (r *Registry) SetHostNotify(fn Callback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hostNotify = fn
	r.notify = fn
}

// setSink installs the notification sink for one run and returns the previous
// one so it can be restored. Nested runs on one registry are not supported;
// the engine gives every conversation its own registry. A nil fn falls back
// to the host sink rather than silencing parked workers.
func (r *Registry) setSink(fn Callback) Callback {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.notify
	if fn == nil {
		fn = r.hostNotify
	}
	r.notify = fn
	return prev
}

func (r *Registry) emitSpawned(role, agentID, instruction string) {
	r.emit(Notification{Kind: NotifySpawned, AgentID: agentID, Role: role, Text: instruction})
}

func (r *Registry) emitFinished(role, agentID, result string, err error) {
	r.emit(Notification{Kind: NotifyFinished, AgentID: agentID, Role: role, Text: result, Err: err})
}

func (r *Registry) sink() Callback {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notify
}

// emit delivers one notification to the current sink (no-op when headless).
func (r *Registry) emit(n Notification) {
	if cb := r.sink(); cb != nil {
		cb(n)
	}
}

// ---------- event mapping ----------

// toolResultNotifyLimit is how much of a tool's stdout the UI event is
// allowed to carry. The model still sees the full result in the transcript;
// this cap is only the copy stored and rendered. exec can dump megabytes
// and the event log is not a second filesystem.
const toolResultNotifyLimit = 64_000

// clipToolResult truncates on runes and keeps newlines. Collapsing them
// used to turn a file body into a one-line dump the UI could not highlight.
func clipToolResult(s string, n int) string {
	r := []rune(s)
	if n > 0 && len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// emitComplete maps one NON-streaming agent event to notifications. Streamed
// assistant messages are handled by streamAcc (the single stream reader);
// this path covers tool results and models that do not stream.
func (r *Registry) emitComplete(role, agentID string, ev *adk.AgentEvent) {
	if ev == nil || ev.Err != nil {
		return
	}
	o := ev.Output
	if o == nil || o.MessageOutput == nil || o.MessageOutput.IsStreaming {
		return
	}
	msg := o.MessageOutput.Message
	if msg == nil {
		return
	}
	switch msg.Role {
	case schema.Assistant:
		if c := strings.TrimSpace(msg.Content); c != "" && len(msg.ToolCalls) == 0 {
			r.emit(Notification{Kind: NotifyAgentMessage, AgentID: agentID, Role: role, Text: c})
		}
		for _, tc := range mergeStreamedToolCalls(msg.ToolCalls) {
			r.emit(Notification{Kind: NotifyToolCall, AgentID: agentID, Role: role,
				Text: tc.Function.Name + "(" + tc.Function.Arguments + ")", ToolCallID: tc.ID})
		}
	case schema.Tool:
		r.emit(Notification{Kind: NotifyToolResult, AgentID: agentID, Role: role,
			Text: clipToolResult(msg.Content, toolResultNotifyLimit), ToolCallID: msg.ToolCallID})
	}
}

// mergeStreamedToolCalls merges incrementally-streamed tool call chunks into
// complete calls (keyed by Index when set, else by ID). Returns one entry per
// actual call with fully concatenated name/arguments, in first-appearance order.
func mergeStreamedToolCalls(chunks []schema.ToolCall) []schema.ToolCall {
	if len(chunks) == 0 {
		return nil
	}
	byKey := map[string]*schema.ToolCall{}
	order := make([]string, 0, len(chunks))
	for _, c := range chunks {
		key := c.ID
		if c.Index != nil {
			key = "i" + itoa(*c.Index)
		}
		e, ok := byKey[key]
		if !ok {
			cp := c
			// Arguments start empty and are concatenated below, including the
			// fragment this very chunk carries — otherwise the first chunk's
			// fragment lands in the buffer twice.
			cp.Function.Arguments = ""
			byKey[key] = &cp
			e = &cp
			order = append(order, key)
		}
		// Some providers resend the full name in every chunk; take the first
		// non-empty instead of concatenating (args remain concatenated).
		if e.Function.Name == "" && c.Function.Name != "" {
			e.Function.Name = c.Function.Name
		}
		if c.Function.Arguments != "" {
			e.Function.Arguments += c.Function.Arguments
		}
		if c.ID != "" && e.ID == "" {
			e.ID = c.ID
		}
	}
	out := make([]schema.ToolCall, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// itoa avoids pulling strconv into the hot merge path's import set purely for
// one small conversion.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}

// ---------- streaming accumulator ----------

// streamAcc drains ONE assistant message stream for one agent. It owns that
// agent's per-turn reasoning/answer accumulators, so NotifyDelta text is
// correct regardless of how the provider chunks the stream, and it resets them
// at every turn boundary. Both the manager loop and each worker goroutine own
// one instance per agent for the whole run.
type streamAcc struct {
	reg     *Registry
	agentID string
	role    string
	handle  *Handle // optional: keeps a worker's activity tail fresh

	// rawEvents also forwards synthetic per-chunk events to Registry.OnEvent,
	// which is the raw (non-UI) hook workers have always exposed.
	rawEvents bool

	mu       sync.Mutex
	turn     int
	answer   strings.Builder
	reason   strings.Builder
	previews map[string]string // last tool_call_delta text per call id
}

// errOutputBudget is a model call that hit its output cap before any answer
// or tool call. Recording that as a finished turn leaves a blank success
// the UI cannot tell from a hang.
var errOutputBudget = errors.New("the model used its whole output budget before it produced an answer")

func outputStoppedEarly(finish string) bool {
	switch strings.ToLower(strings.TrimSpace(finish)) {
	case "length", "max_tokens":
		return true
	default:
		return false
	}
}

// drain consumes st to EOF, emitting notifications as chunks arrive, and
// returns the turn's accumulated answer text plus its merged tool calls. A
// turn with no tool calls is the agent's answer; a turn with tool calls is an
// interim step whose text is commentary. An empty answer that stopped
// because the output budget ran out is an error, not a finished turn.
func (s *streamAcc) drain(st *schema.StreamReader[*schema.Message]) (string, []schema.ToolCall, error) {
	s.beginTurn()

	var chunks []schema.ToolCall
	var finish string
	canceled := false
	for {
		chunk, err := st.Recv()
		if err != nil {
			canceled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
				strings.Contains(err.Error(), "context canceled")
			break
		}
		if chunk == nil {
			continue
		}
		if chunk.ResponseMeta != nil && chunk.ResponseMeta.FinishReason != "" {
			finish = chunk.ResponseMeta.FinishReason
		}
		if rc := chunk.ReasoningContent; rc != "" {
			acc, changed := s.addReasoning(rc)
			if changed {
				s.touch(lastLine(rc))
				s.emit(Notification{Kind: NotifyReasoningDelta, Text: acc})
				s.raw(&schema.Message{Role: schema.Assistant, ReasoningContent: rc})
			}
		}
		if c := chunk.Content; c != "" {
			acc, changed := s.addAnswer(c)
			if changed {
				s.touch(lastLine(c))
				s.emit(Notification{Kind: NotifyDelta, Text: acc})
				s.raw(&schema.Message{Role: schema.Assistant, Content: acc})
			}
		}
		if len(chunk.ToolCalls) > 0 {
			chunks = append(chunks, chunk.ToolCalls...)
			// The call is not issued until this stream ends. Until then the
			// UI has nothing to draw and a long argument looks like a hang.
			s.noteStreamingTools(chunks)
		}
	}

	calls := mergeStreamedToolCalls(chunks)
	answer := s.answerText()
	if canceled {
		// Interrupt during generate is not a finished answer. Emitting one
		// here leaves a complete manager bubble on a turn that will re-enter.
		return answer, nil, nil
	}
	for _, tc := range calls {
		s.touch(tc.Function.Name + "(" + truncStr(tc.Function.Arguments, 80) + ")")
		s.emit(Notification{Kind: NotifyToolCall, ToolCallID: tc.ID,
			Text: tc.Function.Name + "(" + tc.Function.Arguments + ")"})
		s.raw(&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{tc}})
	}
	if len(calls) == 0 && strings.TrimSpace(answer) == "" && outputStoppedEarly(finish) {
		return "", nil, errOutputBudget
	}
	if len(calls) == 0 && strings.TrimSpace(answer) != "" {
		s.emit(Notification{Kind: NotifyAgentMessage, Text: answer})
	}
	return answer, calls, nil
}

// noteStreamingTools tells the UI a tool call is being written. The name and
// the argument size go out as soon as they change; the arguments themselves
// stay in the stream until it ends, because a chapter-sized payload would
// redraw the transcript on every token.
func (s *streamAcc) noteStreamingTools(chunks []schema.ToolCall) {
	for _, tc := range mergeStreamedToolCalls(chunks) {
		name := tc.Function.Name
		if tc.ID == "" || name == "" {
			continue
		}
		text := name + "(" + itoa(utf8.RuneCountInString(tc.Function.Arguments)) + ")"
		if !s.markPreview(tc.ID, text) {
			continue
		}
		s.touch(name)
		s.emit(Notification{Kind: NotifyToolCallDelta, ToolCallID: tc.ID, Text: text})
	}
}

func (s *streamAcc) markPreview(id, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.previews == nil {
		s.previews = map[string]string{}
	}
	if s.previews[id] == text {
		return false
	}
	s.previews[id] = text
	return true
}

func (s *streamAcc) beginTurn() {
	s.mu.Lock()
	s.turn++
	turn := s.turn
	s.answer.Reset()
	s.reason.Reset()
	s.previews = nil
	s.mu.Unlock()
	s.emit(Notification{Kind: NotifyTurn, Text: "turn " + itoa(turn)})
}

func (s *streamAcc) addAnswer(chunk string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeChunk(&s.answer, chunk)
}

func (s *streamAcc) addReasoning(chunk string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeChunk(&s.reason, chunk)
}

// resentMinRunes is the shortest buffer an exact second copy is treated as
// a gateway resending the snapshot. A one-rune or short-token repeat is
// still a repeat; a sentence-sized echo is how the same line floods the
// transcript.
const resentMinRunes = 12

// absorbChunk folds one provider chunk into the text so far.
//
// Deltas are increments ("Hel" then "lo"). Some gateways put the whole
// buffer in every chunk. Concatenating those paints the same sentence
// again on every event. A longer chunk that already starts with the
// buffer is that snapshot. An exact resend of a long buffer is the same
// bug, not the model repeating a short token.
func absorbChunk(prev, chunk string) string {
	if chunk == "" {
		return prev
	}
	if prev == "" {
		return chunk
	}
	if chunk == prev && utf8.RuneCountInString(prev) >= resentMinRunes {
		return prev
	}
	if len(chunk) > len(prev) && strings.HasPrefix(chunk, prev) {
		return chunk
	}
	return prev + chunk
}

func writeChunk(buf *strings.Builder, chunk string) (string, bool) {
	next := absorbChunk(buf.String(), chunk)
	if next == buf.String() {
		return next, false
	}
	buf.Reset()
	buf.WriteString(next)
	return next, true
}

func (s *streamAcc) answerText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answer.String()
}

func (s *streamAcc) emit(n Notification) {
	n.AgentID = s.agentID
	n.Role = s.role
	s.reg.emit(n)
}

func (s *streamAcc) touch(tail string) {
	if s.handle != nil {
		s.handle.setActivity(tail)
	}
}

func (s *streamAcc) raw(msg *schema.Message) {
	if !s.rawEvents || s.reg.OnEvent == nil {
		return
	}
	s.reg.OnEvent(s.role, s.agentID, adk.EventFromMessage(msg, nil, schema.Assistant, ""))
}

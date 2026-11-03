package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// KindComposerPrediction is one suggested next message for the empty composer.
// The chat transcript does not render it. Trace keeps the call and the line.
const KindComposerPrediction = "composer_prediction"

// ComposerPredictionAgentID is the model call's agent id. The offline provider
// keys its script on this same string; a mismatch would run the suggestion as
// a worker.
const ComposerPredictionAgentID = "composer-prediction"

const (
	predictionMaxRunes     = 240
	predictionTailEvents   = 24
	predictionContextRunes = 6000
	predictionCallTimeout  = 30 * time.Second
)

// predictionGenerate is the suggestion call. Tests swap it to force a failure
// or a decline without standing up a broken endpoint.
var predictionGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	return m.Generate(ctx, msgs)
}

// predictionPool drops a suggestion once a newer turn occupies the conversation
// or shutdown has started. The generation is per conversation, not per call.
type predictionPool struct {
	mu      sync.Mutex
	stopped bool
	gen     map[string]uint64
	wg      sync.WaitGroup
}

func newPredictionPool() predictionPool {
	return predictionPool{gen: map[string]uint64{}}
}

func (p *predictionPool) begin(threadID string) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return 0, false
	}
	if p.gen == nil {
		p.gen = map[string]uint64{}
	}
	p.gen[threadID]++
	p.wg.Add(1)
	return p.gen[threadID], true
}

func (p *predictionPool) invalidate(threadID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gen == nil {
		p.gen = map[string]uint64{}
	}
	p.gen[threadID]++
}

func (p *predictionPool) current(threadID string, gen uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.stopped && p.gen[threadID] == gen
}

func (p *predictionPool) done() { p.wg.Done() }

func (p *predictionPool) stop(wait time.Duration) bool {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	finished := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(wait):
		return false
	}
}

// noteTurnOccupied marks any in-flight suggestion stale. A nil engine is a
// hand-built runtime in tests; occupying it is not a conversation moving on.
func (e *Engine) noteTurnOccupied(threadID string) {
	if e == nil {
		return
	}
	e.predictions.invalidate(threadID)
}

// composerPredictionsOn is the config switch, except under go test. An extra
// model call on every turn would change call counts in tests that never opted
// into this feature. Production and the offline app are not tests, so the
// config default applies there.
func (e *Engine) composerPredictionsOn() bool {
	if e == nil || !e.cfg.SwarmSnapshot().ComposerPredictions {
		return false
	}
	if testing.Testing() && !e.composerPredictionsInTest.Load() {
		return false
	}
	return true
}

// scheduleComposerPrediction runs after the turn has decided whether another
// turn already took the conversation. A goal continuation, queued follow-up,
// or unread steer owns that next turn; a placeholder for the one that just
// finished would be stale before it appeared.
func (e *Engine) scheduleComposerPrediction(threadID string, turn *store.Turn, status string, suppressed, nextTurnStarted bool, final string) {
	if e == nil || turn == nil || nextTurnStarted || suppressed || status != store.TurnDone {
		return
	}
	if strings.TrimSpace(final) == "" || !e.composerPredictionsOn() || e.Status(threadID).Running {
		return
	}
	gen, ok := e.predictions.begin(threadID)
	if !ok {
		return
	}
	turnID, userText := turn.ID, turn.UserText
	providerID, modelName := turn.ProviderID, turn.Model
	go func() {
		defer e.predictions.done()
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("next-step suggestion panicked", "turn", turnID, "panic", r)
			}
		}()
		e.runComposerPrediction(threadID, turnID, providerID, modelName, userText, final, gen)
	}()
}

func (e *Engine) runComposerPrediction(threadID, turnID, providerID, modelName, userText, final string, gen uint64) {
	if !e.predictions.current(threadID, gen) || e.Status(threadID).Running {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), predictionCallTimeout)
	defer cancel()
	ctx = provider.WithoutRouteRecovery(ctx)
	events, _, err := e.store.ListTailEvents(threadID, 0, predictionTailEvents)
	if err != nil {
		e.finishPrediction(threadID, turnID, gen, "", err.Error())
		return
	}
	builder, err := e.pool.ModelBuilder(ctx, providerID, modelName, "", e.callRecorder(threadID, turnID))
	if err != nil {
		e.finishPrediction(threadID, turnID, gen, "", err.Error())
		return
	}
	out, err := predictionGenerate(ctx, builder(ComposerPredictionAgentID, ComposerPredictionAgentID), []*schema.Message{
		schema.SystemMessage(predictionPrompt()),
		schema.UserMessage(predictionInput(events, userText, final)),
	})
	if err != nil {
		e.finishPrediction(threadID, turnID, gen, "", err.Error())
		return
	}
	raw := ""
	if out != nil {
		raw = out.Content
	}
	text := sanitizePrediction(raw)
	if text == "" {
		e.finishPrediction(threadID, turnID, gen, "", "no suggestion")
		return
	}
	e.finishPrediction(threadID, turnID, gen, text, "")
}

func (e *Engine) finishPrediction(threadID, turnID string, gen uint64, text, errText string) {
	if !e.predictions.current(threadID, gen) || e.Status(threadID).Running || !e.composerPredictionsOn() {
		return
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindComposerPrediction, AgentID: ComposerPredictionAgentID,
		Text: text, Err: errText,
	})
}

// predictionPrompt is task-agnostic. The conversation is the input; sample
// tasks do not belong here.
func predictionPrompt() string {
	return `Suggest the next message the human would type to continue this conversation.

Reply with that message only. One line. No quotes, no label, no explanation.

The message must:
- be written in the human's language
- be one concrete next step the human would send to the assistant
- be a message to the assistant, not a question back to the human
- stay within 240 characters

If there is no obvious next step, reply with only NONE.`
}

func predictionInput(events []store.Event, user, final string) string {
	var lines []string
	for _, ev := range events {
		switch ev.Kind {
		case KindUser:
			if text := oneLine(plainUserText(ev.Text)); text != "" {
				lines = append(lines, "Human: "+text)
			}
		case "done":
			if text := oneLine(ev.Text); text != "" {
				lines = append(lines, "Assistant: "+text)
			}
		}
	}
	if len(lines) == 0 {
		if text := oneLine(plainUserText(user)); text != "" {
			lines = append(lines, "Human: "+text)
		}
		if text := oneLine(final); text != "" {
			lines = append(lines, "Assistant: "+text)
		}
	}
	return tailRunes(strings.Join(lines, "\n"), predictionContextRunes)
}

func tailRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[len(r)-max:])
}

// sanitizePrediction keeps the first line of a model reply as something the
// human could send. NONE, quotes, and a trailing explanation are not a message.
func sanitizePrediction(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Trim(s, " \t\"'`“”‘’")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || strings.EqualFold(s, "NONE") || strings.EqualFold(s, "NONE.") {
		return ""
	}
	r := []rune(s)
	if len(r) > predictionMaxRunes {
		s = strings.TrimSpace(string(r[:predictionMaxRunes]))
	}
	return s
}

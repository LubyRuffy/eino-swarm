package tui

import (
	"context"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// turnRunner is Registry.RunWith minus the concrete type, so the session
// loop can be tested without a model.
type turnRunner func(ctx context.Context, cfg swarm.RunConfig, emit func(swarm.Notification)) (swarm.RunResult, error)

func registryRun(reg *swarm.Registry) turnRunner {
	return func(ctx context.Context, cfg swarm.RunConfig, emit func(swarm.Notification)) (swarm.RunResult, error) {
		return reg.RunWith(ctx, cfg, func(n swarm.Notification) { emit(n) })
	}
}

// pumpSession drives turns. A one-shot --task run (prompts == nil) starts
// immediately and exits when the swarm is done. Interactive mode waits at
// the composer instead of treating an empty task as an error.
func pumpSession(ctx context.Context, s Session, run turnRunner, out chan<- notificationMsg, prompts <-chan string) {
	defer close(out)
	if run == nil {
		run = registryRun(s.Registry)
	}
	task := strings.TrimSpace(s.Task)
	var messages []adk.Message
	extraRuns := 0
	var turnActivity bool
	for {
		if ctx.Err() != nil {
			return
		}
		if task == "" {
			next, ok := waitPrompt(ctx, prompts)
			if !ok {
				return
			}
			task = next
			messages = continueMessages(messages, task)
			extraRuns = 0
			turnActivity = false
		}

		if err := s.Switcher.Install(); err != nil {
			out <- notificationMsg{Notification: swarm.Notification{
				Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
			if prompts == nil {
				return
			}
			out <- notificationMsg{idle: true}
			task = ""
			extraRuns = 0
			turnActivity = false
			continue
		}

		cfg := sessionConfig(s, task, messages)
		thisRunActivity := false
		res, runErr := run(ctx, cfg, func(n swarm.Notification) {
			out <- notificationMsg{Notification: n}
			if countedGoalToolCall(n) {
				thisRunActivity = true
			}
		})
		turnActivity = turnActivity || thisRunActivity
		if sessionHitIterationCap(ctx, runErr) && sessionShouldContinue(s) {
			if next := dropSystem(res.Transcript); len(next) > 0 {
				messages = next
			}
			continue
		}
		if runErr != nil {
			out <- notificationMsg{Notification: swarm.Notification{
				Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: runErr}}
			if prompts == nil {
				return
			}
			out <- notificationMsg{idle: true}
			task = ""
			extraRuns = 0
			turnActivity = false
			continue
		}
		wasContinuation := extraRuns > 0
		// A continuation with no counted tools must not loop until
		// MaxContinues. The first human-started run still continues once.
		if (!wasContinuation || turnActivity) &&
			sessionShouldContinue(s) && extraRuns < s.MaxContinues {
			extraRuns++
			turnActivity = false
			messages = dropSystem(res.Transcript)
			if text := strings.TrimSpace(s.ContinueTask); text != "" {
				messages = append(messages, schema.UserMessage(text))
				task = text
			}
			continue
		}
		if prompts == nil {
			return
		}
		messages = dropSystem(res.Transcript)
		out <- notificationMsg{idle: true}
		task = ""
		extraRuns = 0
		turnActivity = false
	}
}

func sessionShouldContinue(s Session) bool {
	if s.Plan != nil && (s.Plan.On() || s.Plan.GoalHeld()) {
		return false
	}
	return s.ShouldContinue != nil && s.ShouldContinue()
}

func waitPrompt(ctx context.Context, prompts <-chan string) (string, bool) {
	if prompts == nil {
		return "", false
	}
	for {
		select {
		case <-ctx.Done():
			return "", false
		case next, ok := <-prompts:
			if !ok {
				return "", false
			}
			if text := strings.TrimSpace(next); text != "" {
				return text, true
			}
		}
	}
}

// continueMessages appends a newly typed task onto the prior transcript.
// An empty prior means RunWith should synthesize the first user message
// from Task, so we leave Messages nil.
func continueMessages(prev []adk.Message, task string) []adk.Message {
	if len(prev) == 0 {
		return nil
	}
	out := make([]adk.Message, len(prev), len(prev)+1)
	copy(out, prev)
	return append(out, schema.UserMessage(task))
}

// countedGoalToolCall is progress that should keep TUI auto-continue going.
// Names match engine.ToolCompleteGoal / ToolBlockGoal: those close pursuit
// themselves and must not count as "keep going".
func countedGoalToolCall(n swarm.Notification) bool {
	if n.Kind != swarm.NotifyToolCall {
		return false
	}
	name := strings.TrimSpace(n.Text)
	if i := strings.IndexByte(name, '('); i > 0 {
		name = strings.TrimSpace(name[:i])
	}
	switch name {
	case "complete_goal", "block_goal", "reopen_goal", "":
		return false
	default:
		return true
	}
}

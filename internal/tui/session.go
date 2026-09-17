package tui

import (
	"context"
	"strings"
	"time"

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
	var sessionDeadline time.Time
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
			sessionDeadline = time.Time{}
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
			sessionDeadline = time.Time{}
			continue
		}

		cfg := sessionConfig(s, task, messages)
		runCtx := ctx
		stop := func() {}
		if s.RunTimeout > 0 {
			if sessionDeadline.IsZero() {
				sessionDeadline = time.Now().Add(s.RunTimeout)
			}
			runCtx, stop = context.WithDeadline(ctx, sessionDeadline)
		}
		res, runErr := run(runCtx, cfg, func(n swarm.Notification) {
			out <- notificationMsg{Notification: n}
		})
		stop()
		if sessionHitIterationCap(ctx, runErr) && s.ShouldContinue != nil && s.ShouldContinue() {
			if next := dropSystem(res.Transcript); len(next) > 0 {
				messages = next
			}
			continue
		}
		sessionCap := sessionHitCap(ctx, runCtx, runErr, s.RunTimeout)
		if runErr != nil && !sessionCap {
			out <- notificationMsg{Notification: swarm.Notification{
				Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: runErr}}
			if prompts == nil {
				return
			}
			out <- notificationMsg{idle: true}
			task = ""
			extraRuns = 0
			sessionDeadline = time.Time{}
			continue
		}
		if s.ShouldContinue != nil && s.ShouldContinue() && extraRuns < s.MaxContinues {
			extraRuns++
			messages = dropSystem(res.Transcript)
			if text := strings.TrimSpace(s.ContinueTask); text != "" {
				messages = append(messages, schema.UserMessage(text))
				task = text
			}
			sessionDeadline = time.Time{}
			continue
		}
		if prompts == nil {
			return
		}
		messages = dropSystem(res.Transcript)
		out <- notificationMsg{idle: true}
		task = ""
		extraRuns = 0
		sessionDeadline = time.Time{}
	}
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

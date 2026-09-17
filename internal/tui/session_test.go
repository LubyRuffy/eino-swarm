package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestATurnInstallsTheCurrentModelChoice(t *testing.T) {
	var gotModel, gotEffort string
	sw := &Switcher{
		Model:     "alpha",
		Reasoning: "high",
		Rebuild: func(m, e string) error {
			gotModel, gotEffort = m, e
			return nil
		},
	}
	out := make(chan notificationMsg, 8)
	run := func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
		return swarm.RunResult{}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(context.Background(), Session{Task: "do the thing", Switcher: sw}, run, out, nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("one-shot did not end")
	}
	if gotModel != "alpha" || gotEffort != "high" {
		t.Fatalf("install model=%q effort=%q", gotModel, gotEffort)
	}
}

func TestAFailedModelInstallReturnsTheComposer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 1)
	out := make(chan notificationMsg, 8)
	sw := &Switcher{
		Model: "alpha",
		Rebuild: func(string, string) error {
			return errors.New("the endpoint refused")
		},
	}
	run := func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
		t.Error("a failed install must not start a turn")
		return swarm.RunResult{}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Interactive: true, Switcher: sw}, run, out, prompts)
	}()
	prompts <- "do the thing"
	waitIdle(t, out)
	cancel()
	<-done
}

func TestAnInteractiveSessionWithAnInitialTaskStartsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string)
	out := make(chan notificationMsg, 8)
	var got string
	run := func(_ context.Context, cfg swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		got = cfg.Task
		return swarm.RunResult{Final: "ok"}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Task: "keep the standing objective", Interactive: true}, run, out, prompts)
	}()
	waitIdle(t, out)
	if got != "keep the standing objective" {
		t.Fatalf("task=%q", got)
	}
	cancel()
	<-done
}

func TestAnIdleTUIWaitsForATypedTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string)
	out := make(chan notificationMsg, 16)
	var ran atomic.Int32
	var secondLast string
	run := func(_ context.Context, cfg swarm.RunConfig, emit func(swarm.Notification)) (swarm.RunResult, error) {
		n := int(ran.Add(1))
		if n == 1 && (cfg.Task != "do the thing" || len(cfg.Messages) != 0) {
			t.Errorf("first turn task=%q messages=%d", cfg.Task, len(cfg.Messages))
		}
		if n == 2 {
			if cfg.Task != "and then this" {
				t.Errorf("second turn task=%q", cfg.Task)
			}
			if len(cfg.Messages) > 0 {
				secondLast = cfg.Messages[len(cfg.Messages)-1].Content
			}
		}
		emit(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "ok"})
		return swarm.RunResult{
			Final: "ok",
			Transcript: []adk.Message{
				schema.SystemMessage("sys"),
				schema.UserMessage(cfg.Task),
				schema.AssistantMessage("ok", nil),
			},
		}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Interactive: true}, run, out, prompts)
	}()

	time.Sleep(30 * time.Millisecond)
	if ran.Load() != 0 {
		t.Fatal("a TUI with no task started a run by itself")
	}
	prompts <- "do the thing"
	waitIdle(t, out)
	if ran.Load() != 1 {
		t.Fatalf("runs=%d", ran.Load())
	}
	prompts <- "and then this"
	waitIdle(t, out)
	if ran.Load() != 2 {
		t.Fatalf("second turn missing, runs=%d", ran.Load())
	}
	if secondLast != "and then this" {
		t.Fatalf("the next typed task must be appended to the transcript, last=%q", secondLast)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session loop did not stop")
	}
}

func TestATaskOnTheCommandLineStillRunsOnce(t *testing.T) {
	ctx := context.Background()
	out := make(chan notificationMsg, 8)
	var ran atomic.Int32
	run := func(_ context.Context, cfg swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		ran.Add(1)
		if cfg.Task != "do the thing" {
			t.Errorf("task=%q", cfg.Task)
		}
		return swarm.RunResult{Final: "ok"}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Task: "do the thing"}, run, out, nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("one-shot session did not end")
	}
	if ran.Load() != 1 {
		t.Fatalf("runs=%d", ran.Load())
	}
	if _, ok := <-out; ok {
		t.Fatal("one-shot must close the notification channel so the TUI exits")
	}
}

func TestAnInteractiveErrorReturnsTheComposer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 1)
	out := make(chan notificationMsg, 8)
	run := func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
		return swarm.RunResult{}, errors.New("the endpoint refused")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Interactive: true}, run, out, prompts)
	}()
	prompts <- "do the thing"
	sawErr := false
	timeout := time.After(2 * time.Second)
	for {
		select {
		case n, ok := <-out:
			if !ok {
				t.Fatal("a failed turn closed the TUI instead of returning the composer")
			}
			if n.Notification.Err != nil && strings.Contains(n.Notification.Err.Error(), "refused") {
				sawErr = true
			}
			if n.idle {
				if !sawErr {
					t.Fatal("the failure never reached the UI")
				}
				cancel()
				<-done
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for idle after an error")
		}
	}
}

func TestAGoalAutoContinueHappensBeforeIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 1)
	out := make(chan notificationMsg, 16)
	var tasks []string
	run := func(_ context.Context, cfg swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		tasks = append(tasks, cfg.Task)
		return swarm.RunResult{
			Transcript: []adk.Message{
				schema.UserMessage(cfg.Task),
				schema.AssistantMessage("ok", nil),
			},
		}, nil
	}
	continues := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{
			Interactive:    true,
			ContinueTask:   "keep going",
			MaxContinues:   1,
			ShouldContinue: func() bool { continues++; return true },
		}, run, out, prompts)
	}()
	prompts <- "start"
	waitIdle(t, out)
	if len(tasks) != 2 || tasks[0] != "start" || tasks[1] != "keep going" {
		t.Fatalf("auto-continue tasks=%q", tasks)
	}
	if continues < 1 {
		t.Fatal("ShouldContinue was never asked")
	}
	cancel()
	<-done
}

func TestAGoalIterationCapExtendsWithoutSpendingAutoContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 1)
	out := make(chan notificationMsg, 16)
	var tasks []string
	runs := 0
	run := func(_ context.Context, cfg swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		runs++
		tasks = append(tasks, cfg.Task)
		res := swarm.RunResult{
			Transcript: []adk.Message{
				schema.UserMessage(cfg.Task),
				schema.AssistantMessage("working", nil),
			},
		}
		if runs < 3 {
			return res, errors.New("exceed max iteration")
		}
		return res, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{
			Interactive:    true,
			ContinueTask:   "keep going",
			MaxContinues:   0,
			ShouldContinue: func() bool { return true },
			RunTimeout:     time.Second,
		}, run, out, prompts)
	}()
	prompts <- "start"
	waitIdle(t, out)
	if runs != 3 {
		t.Fatalf("ReAct slices must extend in place, runs=%d tasks=%q", runs, tasks)
	}
	for _, task := range tasks {
		if task != "start" {
			t.Fatalf("an iteration slice must not inject the continue prompt: %q", tasks)
		}
	}
	cancel()
	<-done
}

func TestASessionCapDoesNotKillAnInteractiveTUI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 1)
	out := make(chan notificationMsg, 8)
	run := func(runCtx context.Context, _ swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		<-runCtx.Done()
		return swarm.RunResult{
			Transcript: []adk.Message{schema.UserMessage("do the thing")},
		}, runCtx.Err()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Interactive: true, RunTimeout: 30 * time.Millisecond}, run, out, prompts)
	}()
	prompts <- "do the thing"
	waitIdle(t, out)
	cancel()
	<-done
}

func TestAOneShotErrorEndsTheSession(t *testing.T) {
	out := make(chan notificationMsg, 8)
	run := func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
		return swarm.RunResult{}, errors.New("the endpoint refused")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(context.Background(), Session{Task: "do the thing"}, run, out, nil)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("one-shot error did not end")
	}
	var sawErr bool
	for n := range out {
		if n.Notification.Err != nil && strings.Contains(n.Notification.Err.Error(), "refused") {
			sawErr = true
		}
		if n.idle {
			t.Fatal("one-shot must not go idle after a fatal error")
		}
	}
	if !sawErr {
		t.Fatal("the failure never reached the UI")
	}
}

func TestACanceledSessionDoesNotStartATurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan notificationMsg, 1)
	var ran atomic.Int32
	run := func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
		ran.Add(1)
		return swarm.RunResult{}, nil
	}
	pumpSession(ctx, Session{Task: "do the thing"}, run, out, nil)
	if ran.Load() != 0 {
		t.Fatal("a canceled session started a run")
	}
}

func TestBlankPromptsAreIgnoredUntilThereIsATask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prompts := make(chan string, 2)
	out := make(chan notificationMsg, 8)
	var ran atomic.Int32
	run := func(_ context.Context, cfg swarm.RunConfig, _ func(swarm.Notification)) (swarm.RunResult, error) {
		ran.Add(1)
		if cfg.Task != "do the thing" {
			t.Errorf("task=%q", cfg.Task)
		}
		return swarm.RunResult{}, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(ctx, Session{Interactive: true}, run, out, prompts)
	}()
	prompts <- "   "
	prompts <- "do the thing"
	waitIdle(t, out)
	if ran.Load() != 1 {
		t.Fatalf("runs=%d", ran.Load())
	}
	cancel()
	<-done
}

func TestClosingTheComposerEndsTheSession(t *testing.T) {
	out := make(chan notificationMsg)
	prompts := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpSession(context.Background(), Session{Interactive: true}, func(context.Context, swarm.RunConfig, func(swarm.Notification)) (swarm.RunResult, error) {
			t.Error("closed composer started a run")
			return swarm.RunResult{}, nil
		}, out, prompts)
	}()
	close(prompts)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the composer did not end the session")
	}
}

func waitIdle(t *testing.T, out <-chan notificationMsg) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case n, ok := <-out:
			if !ok {
				t.Fatal("session ended before going idle")
			}
			if n.idle {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for idle")
		}
	}
}

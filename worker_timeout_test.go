package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// A finished watchdog must not give the same stalled worker unlimited fresh deadlines.
func TestRepeatedWatchdogStopsTheFourthWorkerRun(t *testing.T) {
	for _, prune := range []bool{false, true} {
		t.Run(map[bool]string{false: "live-handle", true: "archived-handle"}[prune], func(t *testing.T) {
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			reg.AgentTimeout = 20 * time.Millisecond
			var builds atomic.Int32
			reg.ModelBuilder = func(role, id string) model.BaseChatModel {
				builds.Add(1)
				return &scriptedModel{turns: workerTurns(nil)}
			}
			extra := &workTool{d: time.Second}
			h, err := reg.Spawn(context.Background(), "worker", "perform work", reg.ModelBuilder, extra)
			if err != nil {
				t.Fatal(err)
			}
			id := h.ID
			for run := 0; run < 3; run++ {
				select {
				case <-h.Done():
				case <-time.After(time.Second):
					t.Fatal("watchdog did not finish")
				}
				_, err, done := h.Result()
				if !done || err == nil || !strings.Contains(err.Error(), "timeout") {
					t.Fatalf("run %d: done=%v error=%v", run, done, err)
				}
				if prune {
					reg.Stats()
				}
				if run < 2 {
					h, err = reg.Resume(context.Background(), id, "continue", reg.ModelBuilder, extra)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			next, err := reg.Resume(context.Background(), id, "continue", reg.ModelBuilder, extra)
			if err == nil {
				next.Cancel()
				<-next.Done()
				t.Fatal("fourth run started after three consecutive watchdog failures")
			}
			if builds.Load() != 3 {
				t.Fatalf("worker starts=%d want 3", builds.Load())
			}
		})
	}
}

func TestWorkerRecoveryCannotReopenAClosedOrFullRegistry(t *testing.T) {
	closed := NewRegistry()
	closed.Close()
	if h, err := closed.claimForResume(&agentPast{ID: "worker-1", TimeoutFailures: 2}, true); h != nil || err == nil || !strings.Contains(err.Error(), "registry closed") {
		t.Fatalf("closed recovery h=%v error=%v", h, err)
	}

	full := NewRegistry()
	t.Cleanup(full.Close)
	full.MaxConcurrent = 1
	running, err := full.Spawn(t.Context(), "busy", "perform work", func(role, id string) model.BaseChatModel {
		return &scriptedModel{turns: workerTurns(nil)}
	}, &workTool{d: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	past := &agentPast{ID: "worker-2", Role: "worker", TimeoutFailures: 2}
	h, err := full.claimForResume(past, true)
	var atCapacity *atCapacityError
	if h != nil || !errors.As(err, &atCapacity) || past.TimeoutFailures != 2 {
		t.Fatalf("full recovery h=%v error=%v counter=%d", h, err, past.TimeoutFailures)
	}
	running.Cancel()
	<-running.Done()
}

func TestWorkerWatchdogClassificationAndReset(t *testing.T) {
	for _, tt := range []struct {
		name            string
		err, watch, run error
		want            int
	}{
		{"watchdog", context.DeadlineExceeded, context.DeadlineExceeded, nil, 3},
		{"cancelled", context.Canceled, context.DeadlineExceeded, context.Canceled, 0},
		{"provider-deadline", context.DeadlineExceeded, nil, nil, 0},
		{"success", nil, nil, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handle{ID: "worker-1", timeoutFailures: 2}
			h.completeWorker("result", tt.err, time.Second, tt.watch, tt.run)
			if h.timeoutFailures != tt.want || h.finished.IsZero() {
				t.Fatalf("finish=%+v", h)
			}
			var timeout *WorkerTimeoutError
			if got := errors.As(h.err, &timeout); got != (tt.want == 3) {
				t.Fatalf("wrong error category %v", h.err)
			}
			if timeout != nil && (!strings.Contains(timeout.Error(), "3 consecutive") || !errors.Is(timeout, tt.err)) {
				t.Fatalf("watchdog error=%v", timeout)
			}
		})
	}
}

func TestWorkerTimeoutLimitStopsManagerResumeAndSameRoleSpawn(t *testing.T) {
	for _, name := range []string{"resume_agent", "spawn_agent"} {
		t.Run(name, func(t *testing.T) {
			reg := NewRegistry()
			defer reg.Close()
			reg.AgentTimeout = 20 * time.Millisecond
			reg.SubAgentTools = []tool.BaseTool{&workTool{d: time.Second}}
			var starts atomic.Int32
			reg.ModelBuilder = func(role, id string) model.BaseChatModel {
				starts.Add(1)
				return &scriptedModel{turns: workerTurns(nil)}
			}
			var managerCalls atomic.Int32
			manager := &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{func(_ int, _ []*schema.Message) *schema.Message {
				n := int(managerCalls.Add(1) - 1)
				toolName, args := "spawn_agent", `{"role":"worker","task":"perform work"}`
				if n%2 == 1 {
					toolName, args = "wait_agents", `{"agent_ids":["worker-1"],"timeout_s":1}`
				} else if n > 0 && name == "resume_agent" {
					toolName, args = name, `{"agent_id":"worker-1","task":"continue"}`
				}
				return schema.AssistantMessage("", []schema.ToolCall{rawCall(fmt.Sprintf("call-%d", n), toolName, args)})
			}}}
			rec := &recorder{}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
			defer cancel()
			_, err := reg.RunWith(ctx, RunConfig{Model: manager, Task: "complete the work", MaxIterations: 20}, rec.cb())
			var exhausted *WorkerTimeoutLimitError
			if !errors.As(err, &exhausted) || exhausted.Failures != 3 || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("manager did not stop at the worker budget: %v", err)
			}
			if starts.Load() != 3 || len(rec.ofKind(NotifySpawned)) != 3 || len(rec.ofKind(NotifyFinished)) != 3 {
				t.Fatalf("starts=%d notifications=%v", starts.Load(), rec.all())
			}
		})
	}
}

func TestExhaustedWorkerCannotRestoreOrRaceIntoAnotherRun(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(FinishedWorker{ID: "worker-1", Role: "worker", TimeoutFailures: 3}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Resume(context.Background(), "worker-1", "continue", nil)
			var exhausted *WorkerTimeoutLimitError
			if !errors.As(err, &exhausted) {
				t.Errorf("race bypassed guard: %v", err)
			}
		}()
	}
	wg.Wait()
	for _, prune := range []bool{false, true} {
		if prune {
			reg.Stats()
		}
		_, err := reg.Restore(context.Background(), RestoredWorker{ID: "worker-1", Role: "worker", Task: "continue"}, nil)
		var exhausted *WorkerTimeoutLimitError
		if !errors.As(err, &exhausted) {
			t.Fatalf("Restore reset the guard: %v", err)
		}
	}
	reg.ResetWorkerTimeouts()
	h, err := reg.Resume(context.Background(), "worker-1", "smaller task", oneShot("done"))
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	if result, err, _ := h.Result(); result != "done" || err != nil {
		t.Fatalf("explicit retry=%s %v", result, err)
	}
}

func TestSuccessfulWorkerClearsEarlierTimeoutBudget(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(FinishedWorker{ID: "worker-1", Role: "worker", TimeoutFailures: 2}); err != nil {
		t.Fatal(err)
	}
	h, err := reg.Resume(context.Background(), "worker-1", "perform smaller work", oneShot("done"))
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	reg.Stats()
	p, err := reg.recall("worker-1")
	if err != nil || p.TimeoutFailures != 0 {
		t.Fatalf("success did not reset archive: %+v %v", p, err)
	}
}

func TestHumanTimeoutResetKeepsWorkersAndTheirDeadlines(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(FinishedWorker{ID: "done-1", Role: "done", TimeoutFailures: 3}); err != nil {
		t.Fatal(err)
	}
	reg.AgentTimeout = time.Second
	h, err := reg.Spawn(context.Background(), "running", "perform work", func(string, string) model.BaseChatModel { return &scriptedModel{turns: workerTurns(nil)} }, &workTool{d: time.Second * 2})
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.timeoutFailures = 2
	h.mu.Unlock()
	reg.ResetWorkerTimeouts()
	h.mu.Lock()
	count := h.timeoutFailures
	h.mu.Unlock()
	if count != 0 {
		t.Fatal("live timeout counter did not reset")
	}
	if _, _, done := h.Result(); done {
		t.Fatal("human reset cancelled the worker")
	}
	p, err := reg.recall("done-1")
	if err != nil || p.TimeoutFailures != 0 {
		t.Fatal("archive did not reset")
	}
	h.Cancel()
	<-h.Done()
}

type watchdogStreamModel struct{}

func (*watchdogStreamModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*watchdogStreamModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](2)
	go func() {
		defer sw.Close()
		sw.Send(&schema.Message{Role: schema.Assistant, ReasoningContent: "still reasoning"}, nil)
		<-ctx.Done()
		sw.Send(nil, ctx.Err())
	}()
	return sr, nil
}
func TestStreamingWorkerWatchdogKeepsItsCauseAndCount(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	reg.AgentTimeout = 20 * time.Millisecond
	h, err := reg.Spawn(context.Background(), "worker", "perform work", func(string, string) model.BaseChatModel { return &watchdogStreamModel{} })
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	_, err, _ = h.Result()
	var timeout *WorkerTimeoutError
	if !errors.As(err, &timeout) || timeout.Consecutive != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stream lost its watchdog category: %v", err)
	}
}

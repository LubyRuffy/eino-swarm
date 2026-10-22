package swarm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestWorkerDefaultAllowsAnHour(t *testing.T) {
	if DefaultAgentTimeout != time.Hour {
		t.Fatalf("worker default=%s, want one hour", DefaultAgentTimeout)
	}
}

type deadlineSettingsModel struct {
	watchdogStreamModel
	seen chan context.Context
}

func (m *deadlineSettingsModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.seen <- ctx
	return m.watchdogStreamModel.Stream(ctx, in, opts...)
}

func TestTimeoutSettingsAffectOnlyTheNextWorkerLifetime(t *testing.T) {
	r := NewRegistry()
	defer r.Close()
	seen := make(chan context.Context, 8)
	builder := func(string, string) model.BaseChatModel { return &deadlineSettingsModel{seen: seen} }
	r.SetAgentTimeout(2 * time.Second)
	h, err := r.Spawn(t.Context(), "worker", "perform work", builder)
	if err != nil {
		t.Fatal(err)
	}
	first := <-seen
	before, ok := first.Deadline()
	if !ok {
		t.Fatal("no watchdog deadline")
	}
	r.SetAgentTimeout(5 * time.Second)
	after, _ := first.Deadline()
	if !before.Equal(after) {
		t.Fatal("save extended active lifetime")
	}
	h.Cancel()
	<-h.Done()
	h, err = r.Resume(t.Context(), h.ID, "continue", builder)
	if err != nil {
		t.Fatal(err)
	}
	next := <-seen
	d, _ := next.Deadline()
	if time.Until(d) < 4*time.Second {
		t.Fatal("next lifetime kept old timeout")
	}
	h.Cancel()
	<-h.Done()
	r.SetAgentTimeout(0)
	if r.agentTimeout() != time.Hour {
		t.Fatal("unset no longer defaults to hour")
	}
	h, err = r.Spawn(t.Context(), "default-worker", "perform work", builder)
	if err != nil {
		t.Fatal(err)
	}
	defaultCtx := <-seen
	defaultDeadline, _ := defaultCtx.Deadline()
	if time.Until(defaultDeadline) < 59*time.Minute {
		t.Fatal("actual default worker did not receive one hour")
	}
	h.Cancel()
	<-h.Done()
	if err := r.PlantFinished(FinishedWorker{ID: "blocked", Role: "blocked", TimeoutFailures: 3}); err != nil {
		t.Fatal(err)
	}
	r.SetAgentTimeout(time.Hour)
	_, err = r.Resume(t.Context(), "blocked", "continue", builder)
	var limit *WorkerTimeoutLimitError
	if !errors.As(err, &limit) {
		t.Fatal("timeout edit reset recovery budget", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i < 100; i++ {
			r.SetAgentTimeout(time.Duration(i) * time.Second)
			r.SetMaxTurns(i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = r.agentTimeout()
			_ = r.turnsLimit()
		}
	}()
	wg.Wait()
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			r.SetAgentTimeout(time.Hour)
			r.SetMaxTurns(20)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			h, err := r.Spawn(context.Background(), "parallel", "perform work", func(string, string) model.BaseChatModel { return &scriptedModel{turns: workerTurns(nil)} })
			if err != nil {
				t.Error(err)
				return
			}
			h.Cancel()
			<-h.Done()
		}
	}()
	wg.Wait()
}

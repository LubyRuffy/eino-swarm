package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type outputIdentityModel struct {
	steps <-chan *schema.Message
}

func (m *outputIdentityModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("streaming required")
}

func (m *outputIdentityModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r, w := schema.Pipe[*schema.Message](0)
	go func() {
		defer w.Close()
		if m.steps == nil {
			w.Send(schema.AssistantMessage("supervision complete", nil), nil)
			return
		}
		for {
			select {
			case <-ctx.Done():
				w.Send(nil, ctx.Err())
				return
			case msg, ok := <-m.steps:
				if !ok || w.Send(msg, nil) {
					return
				}
			}
		}
	}()
	return r, nil
}

func eventMessageID(t *testing.T, ev store.Event) string {
	t.Helper()
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	return wire.MessageID
}

// A worker may still be in one model output while two manager checks finish.
// Each durable snapshot must retain that output's identity, even after flush
// and replacement of the manager's accumulator/sink.
func TestWorkerOutputIdentitySurvivesManagerCheckpoints(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	acc := newAccumulator(e, th.ID, "manager-first", time.Hour)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	reg.SetHostNotify(acc.onNotify)
	steps := make(chan *schema.Message, 4)
	reg.ModelBuilder = func(string, string) model.BaseChatModel { return &outputIdentityModel{steps: steps} }
	h, err := reg.Spawn(context.Background(), "inspector", "inspect the current evidence", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	steps <- &schema.Message{Role: schema.Assistant, ReasoningContent: "checking"}
	// Flushing the held delta also waits for onNotify to accept the chunk.
	waitWorkerOutput(t, sub, acc, "checking")
	acc.flushAll()
	first, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint store.Event
	for _, ev := range first {
		if ev.Kind == KindReasoning && ev.AgentID == h.ID {
			checkpoint = ev
		}
	}
	id := eventMessageID(t, checkpoint)
	if id == "" {
		t.Fatalf("checkpoint omitted its streamed output identity: %+v", checkpoint)
	}
	for i, turnID := range []string{"manager-first", "manager-second"} {
		if i > 0 {
			acc = newAccumulator(e, th.ID, turnID, time.Hour)
			reg.SetHostNotify(acc.onNotify)
		}
		if _, err := reg.RunWith(context.Background(), swarm.RunConfig{
			Task: "check current progress", Model: &outputIdentityModel{},
		}, swarm.Callback(acc.onNotify)); err != nil {
			t.Fatal(err)
		}
		steps <- &schema.Message{Role: schema.Assistant, ReasoningContent: " further"}
		want := "checking further"
		if i > 0 {
			want += " further"
		}
		live := waitWorkerOutput(t, sub, acc, want)
		if eventMessageID(t, live) != id {
			t.Fatalf("worker output changed identity across manager boundary: %+v", live)
		}
		acc.flushAll()
	}
	close(steps)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := reg.Wait(ctx, 0); err != nil {
		t.Fatal(err)
	}
	acc.flushAll()
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, ev := range events {
		if ev.Kind == KindReasoning && ev.AgentID == h.ID {
			count++
			if eventMessageID(t, ev) != id {
				t.Fatalf("stored checkpoint lost output identity: %+v", ev)
			}
		}
	}
	if count != 3 {
		t.Fatalf("want three durable same-output checkpoints, got %d", count)
	}
}

func waitWorkerOutput(t *testing.T, sub *Subscription, acc *accumulator, text string) store.Event {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			acc.flushAllLive()
		case ev := <-sub.C:
			if ev.Kind == "reasoning_delta" && ev.Text == text {
				return ev
			}
		case <-deadline.C:
			t.Fatalf("worker did not stream %q", text)
		}
	}
}

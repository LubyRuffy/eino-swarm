package swarm

import (
	"context"
	"errors"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// InterruptedToolResult is the synthetic tool output when the human aborts
// an in-flight manager tool so unread steering can land on the next model
// call of the same run. Task-agnostic on purpose.
const InterruptedToolResult = "the user interrupted this tool before it finished"

// SteerSeqKey is the Extra slot that tags a queued manager steer with the
// event seq the engine assigned, so one unread bubble can be retracted.
const SteerSeqKey = "steer_seq"

// Preempt cancels in-flight manager tool calls and the current generate
// without closing the registry or cancelling workers. Steering already in
// the inbox is delivered by the next BeforeModel. False when nothing is
// running to preempt.
func (r *Registry) Preempt() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.preempted = true
	if len(r.epochs) == 0 {
		r.preemptPending = true
	}
	for _, cancel := range r.epochs {
		cancel()
	}
	return true
}

// TakePreempt reports and clears whether Preempt was called since the last
// take. The engine uses it to re-enter a generate that died on epoch
// cancel without treating a provider "context canceled" as a human abort.
func (r *Registry) TakePreempt() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.preempted
	r.preempted = false
	// A Preempt with no live epoch arms the next bindEpoch. If this run
	// already finished, that bomb must not fire on the next turn.
	r.preemptPending = false
	return v
}

// WithEpoch binds a cancelable slice of ctx so Preempt can abort a wait
// the same way it aborts a generate. Call stop when the wait returns.
func (r *Registry) WithEpoch(ctx context.Context) (context.Context, func()) {
	ctx, id := r.bindEpoch(ctx)
	return ctx, func() { r.dropEpoch(id) }
}

// HasPendingSteers reports unread manager steering still sitting in the inbox.
func (r *Registry) HasPendingSteers() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.mgrInbox) > 0
}

// RetractManagerSteer drops one unread steering message by the event seq
// tagged on it. False when the registry is closed, the seq is unknown, or
// the manager already consumed that steer.
func (r *Registry) RetractManagerSteer(seq int64) bool {
	if seq <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	for i, m := range r.mgrInbox {
		if SteerSeq(m) != seq {
			continue
		}
		r.mgrInbox = append(r.mgrInbox[:i], r.mgrInbox[i+1:]...)
		return true
	}
	return false
}

// SetSteerSeq tags a queued steering message so RetractManagerSteer can
// find that one bubble. The engine assigns the seq when it records the
// steer event.
func SetSteerSeq(msg *schema.Message, seq int64) {
	if msg == nil || seq <= 0 {
		return
	}
	if msg.Extra == nil {
		msg.Extra = map[string]any{}
	}
	msg.Extra[SteerSeqKey] = seq
}

// SteerSeq reads the event seq tagged by SetSteerSeq. Zero means untagged.
func SteerSeq(msg *schema.Message) int64 {
	if msg == nil || msg.Extra == nil {
		return 0
	}
	switch v := msg.Extra[SteerSeqKey].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func (r *Registry) bindEpoch(ctx context.Context) (context.Context, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epochSeq++
	id := r.epochSeq
	child, cancel := context.WithCancel(ctx)
	if r.preemptPending {
		r.preemptPending = false
		cancel()
	}
	if r.epochs == nil {
		r.epochs = map[uint64]context.CancelFunc{}
	}
	r.epochs[id] = cancel
	return child, id
}

func (r *Registry) dropEpoch(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.epochs, id)
}

func (r *Registry) preemptToolResult(parent, epoch context.Context, out string, err error) (string, error) {
	if epoch.Err() == nil || parent.Err() != nil {
		return out, err
	}
	return InterruptedToolResult, nil
}

type epochModel struct {
	inner model.BaseModel[*schema.Message]
	reg   *Registry
}

func (m *epochModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	ctx, id := m.reg.bindEpoch(ctx)
	defer m.reg.dropEpoch(id)
	return m.inner.Generate(ctx, input, opts...)
}

func (m *epochModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	ctx, id := m.reg.bindEpoch(ctx)
	sr, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		m.reg.dropEpoch(id)
		return nil, err
	}
	return wrapEpochStream(ctx, sr, func() { m.reg.dropEpoch(id) }), nil
}

func wrapEpochStream(ctx context.Context, sr *schema.StreamReader[*schema.Message], done func()) *schema.StreamReader[*schema.Message] {
	out, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer done()
		defer sw.Close()
		// Close the inner reader only after Recv has returned. eino's
		// closeRecv signals the writer; it does not unblock an in-flight
		// Recv. Closing during Recv deadlocks copied streams (nested
		// Once) and leaves Interrupt hanging on the generate.
		defer func() {
			defer func() { _ = recover() }()
			sr.Close()
		}()
		for {
			msg, err := sr.Recv()
			if err != nil {
				if ctx.Err() != nil {
					sw.Send(nil, ctx.Err())
				} else if !errors.Is(err, io.EOF) {
					sw.Send(nil, err)
				}
				return
			}
			if ctx.Err() != nil {
				sw.Send(nil, ctx.Err())
				return
			}
			if sw.Send(msg, nil) {
				return
			}
		}
	}()
	return out
}

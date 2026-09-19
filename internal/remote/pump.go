package remote

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
)

// lagCheckInterval matches the SSE path: a stored event dropped for a
// slow phone must be replayed, not forgotten.
var lagCheckInterval = 200 * time.Millisecond

// watchLagged is the subscriber overflow check. Tests force it so the
// ticker path does not depend on filling engine.subscriberBuffer.
var watchLagged = func(sub *engine.Subscription) bool { return sub.Lagged() }

// LinkPump is one device connection: RPC on Recv, watch pushes on Send.
type LinkPump struct {
	eng       *engine.Engine
	cfg       config.RemoteConfig
	path      string
	sessionID string

	sendMu sync.Mutex
	send   func([]byte) error

	mu       sync.Mutex
	cancel   context.CancelFunc
	watching string
}

func newLinkPump(eng *engine.Engine, cfg config.RemoteConfig, l *client.Link) *LinkPump {
	return &LinkPump{
		eng:       eng,
		cfg:       cfg,
		path:      l.Path(),
		sessionID: l.SessionID(),
		send:      l.Send,
	}
}

func (p *LinkPump) Close() {
	p.stopWatch()
}

func (p *LinkPump) reply(resp Response) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	p.sendRaw(raw)
}

func (p *LinkPump) sendRaw(raw []byte) {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	_ = p.send(raw)
}

// Dispatch handles one inbound pairlink payload.
func (p *LinkPump) Dispatch(msg []byte) {
	var req Request
	if err := json.Unmarshal(msg, &req); err != nil {
		p.reply(fail("", p.path, p.sessionID, "bad_request", "not json"))
		return
	}
	switch strings.TrimSpace(req.Op) {
	case OpWatch:
		p.reply(p.startWatch(req))
	case OpUnwatch:
		p.stopWatch()
		p.reply(okBase(req.ID, p.path, p.sessionID))
	default:
		p.reply(Handle(p.eng, p.cfg, req, p.path, p.sessionID))
	}
}

func (p *LinkPump) startWatch(req Request) Response {
	tid := strings.TrimSpace(req.ThreadID)
	if tid == "" {
		return fail(req.ID, p.path, p.sessionID, "bad_request", "thread_id required")
	}
	if _, err := p.eng.Store().GetThread(tid); err != nil {
		return mapErr(req.ID, p.path, p.sessionID, err)
	}
	p.stopWatch()
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.watching = tid
	p.mu.Unlock()
	sub := p.eng.Subscribe(tid)
	go p.runWatch(ctx, sub, tid, req.Since)
	return okBase(req.ID, p.path, p.sessionID)
}

func (p *LinkPump) stopWatch() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.watching = ""
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *LinkPump) runWatch(ctx context.Context, sub *engine.Subscription, threadID string, since int64) {
	defer sub.Close()
	highest := since
	if err := p.catchUp(ctx, threadID, &highest); err != nil {
		if ctx.Err() != nil {
			return
		}
		p.reply(fail("", p.path, p.sessionID, "", fmtErr(err)))
		return
	}
	if ctx.Err() != nil {
		return
	}
	st := p.eng.Status(threadID)
	p.reply(Response{
		V:         ProtocolV,
		OK:        true,
		Op:        OpReady,
		Path:      p.path,
		SessionID: p.sessionID,
		ThreadID:  threadID,
		Seq:       highest,
		Status: &WatchStatus{
			Running:        st.Running,
			TurnID:         st.TurnID,
			AwaitingAnswer: st.AwaitingAnswer,
		},
	})
	ticker := time.NewTicker(lagCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if watchLagged(sub) {
				p.reply(Response{
					V:         ProtocolV,
					OK:        true,
					Op:        OpLagged,
					Path:      p.path,
					SessionID: p.sessionID,
					ThreadID:  threadID,
					Seq:       highest,
				})
				_ = p.catchUp(ctx, threadID, &highest)
			}
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if ev.Seq > 0 && ev.Seq <= highest {
				continue
			}
			if !p.pushEvent(threadID, ev) {
				continue
			}
			if ev.Seq > highest {
				highest = ev.Seq
			}
		}
	}
}

func (p *LinkPump) catchUp(ctx context.Context, threadID string, highest *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	history, err := p.eng.Replay(threadID, *highest)
	if err != nil {
		return err
	}
	for _, ev := range history {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !p.pushEvent(threadID, ev) {
			continue
		}
		if ev.Seq > *highest {
			*highest = ev.Seq
		}
	}
	return nil
}

func (p *LinkPump) pushEvent(threadID string, ev store.Event) bool {
	if !shouldPush(ev.Kind) {
		return ev.Seq > 0
	}
	raw, ok := encodeEventPush(p.path, p.sessionID, threadID, ev, p.cfg)
	if !ok {
		return ev.Seq > 0
	}
	p.sendRaw(raw)
	return true
}

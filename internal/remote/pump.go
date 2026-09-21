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

// readWatchHistory is last-turn (or replay) for a new watch. Tests fail it
// after GetThread so a closed store cannot hide this path — GetThread
// would 404 first.
var readWatchHistory = func(p *LinkPump, threadID string, since int64) ([]store.Event, bool, error) {
	return p.watchHistory(threadID, since)
}

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
	watchWG  sync.WaitGroup
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
	sub := p.eng.Subscribe(tid)
	p.mu.Lock()
	p.cancel = cancel
	p.watching = tid
	p.mu.Unlock()
	history, hasMore, err := readWatchHistory(p, tid, req.Since)
	if err != nil {
		cancel()
		sub.Close()
		p.mu.Lock()
		if p.watching == tid {
			p.cancel = nil
			p.watching = ""
		}
		p.mu.Unlock()
		return fail(req.ID, p.path, p.sessionID, "", fmtErr(err))
	}
	highest := watchHighest(req.Since, history, hasMore, p.eng.Store(), tid)
	resp := p.readySnapshot(req.ID, tid, history, hasMore, highest)
	p.watchWG.Add(1)
	go func() {
		defer p.watchWG.Done()
		defer sub.Close()
		p.watchLoop(ctx, sub, tid, highest)
	}()
	return resp
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
	p.watchWG.Wait()
}

func (p *LinkPump) runWatch(ctx context.Context, sub *engine.Subscription, threadID string, since int64) {
	defer sub.Close()
	history, hasMore, err := p.loadHistory(ctx, threadID, since)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.reply(fail("", p.path, p.sessionID, "", fmtErr(err)))
		return
	}
	if ctx.Err() != nil {
		return
	}
	highest := watchHighest(since, history, hasMore, p.eng.Store(), threadID)
	p.reply(p.readySnapshot("", threadID, history, hasMore, highest))
	p.watchLoop(ctx, sub, threadID, highest)
}

func (p *LinkPump) readySnapshot(id, threadID string, history []store.Event, hasMore bool, highest int64) Response {
	packed := packLogEvents(id, p.path, p.sessionID, threadID, history, hasMore, p.cfg)
	st := p.eng.Status(threadID)
	packed.Op = OpReady
	packed.Seq = highest
	packed.Status = &WatchStatus{
		Running:        st.Running,
		TurnID:         st.TurnID,
		AwaitingAnswer: st.AwaitingAnswer,
	}
	return packed
}

func watchHighest(since int64, history []store.Event, hasMore bool, st *store.Store, threadID string) int64 {
	highest := since
	for _, ev := range history {
		if ev.Seq > highest {
			highest = ev.Seq
		}
	}
	if highest > since || !hasMore || st == nil {
		return highest
	}
	tail, _, err := st.ListTailEvents(threadID, 0, 1)
	if err != nil || len(tail) == 0 {
		return highest
	}
	if tail[len(tail)-1].Seq > highest {
		return tail[len(tail)-1].Seq
	}
	return highest
}

func (p *LinkPump) watchLoop(ctx context.Context, sub *engine.Subscription, threadID string, highest int64) {
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
				_, _ = p.catchUp(ctx, threadID, &highest)
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

func (p *LinkPump) loadHistory(ctx context.Context, threadID string, since int64) ([]store.Event, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return p.watchHistory(threadID, since)
}

func (p *LinkPump) catchUp(ctx context.Context, threadID string, highest *int64) (bool, error) {
	history, hasMore, err := p.loadHistory(ctx, threadID, *highest)
	if err != nil {
		return false, err
	}
	for _, ev := range history {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !p.pushEvent(threadID, ev) {
			continue
		}
		if ev.Seq > *highest {
			*highest = ev.Seq
		}
	}
	return hasMore, nil
}

// First open (since 0) is the last turn, capped at watch_events from
// that turn's end. Replaying from seq 0 paints the oldest user message
// on a phone that cannot scroll the rest in time. Pull-up uses log.
func (p *LinkPump) watchHistory(threadID string, since int64) ([]store.Event, bool, error) {
	if since > 0 {
		events, err := p.eng.Replay(threadID, since)
		return events, false, err
	}
	return lastTurnWindow(p.eng.Store(), threadID, watchEvents(p.cfg))
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

package engine

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindRewound is broadcast when a conversation is truncated so a user message
// can be resent from that position. It is not stored: a reload already sees
// the shortened log, and a live client needs the cut point to drop the rows
// that just vanished.
const KindRewound = "rewound"

var rewindWait = 30 * time.Second

func (e *Engine) peekRewind(threadID string, fromSeq int64) ([]store.ImageRef, error) {
	ev, err := e.store.GetEvent(threadID, fromSeq)
	if err != nil {
		return nil, err
	}
	if ev.Kind != KindUser {
		return nil, ErrNotRewindable
	}
	return append([]store.ImageRef(nil), ev.Images...), nil
}

func (e *Engine) applyRewind(threadID string, fromSeq int64) error {
	rt := e.runtimeFor(threadID)
	if st := rt.status(); st.Running {
		runningID := st.TurnID
		rt.interrupt()
		rt.waitIdle(rewindWait)
		if rt.status().Running {
			return fmt.Errorf("engine: the running turn did not stop in time")
		}
		// waitIdle is the runtime going idle, which happens *before* the
		// terminal event is recorded. Truncating in that window lets done
		// land after the cut. Wait until FinishTurn so that row is in, then
		// delete it with everything else from this seq.
		e.waitTurnClosed(runningID, rewindWait)
	}

	cut, err := e.store.GetEvent(threadID, fromSeq)
	if err != nil {
		return err
	}
	turn, err := e.store.GetTurn(cut.TurnID)
	if err != nil {
		return err
	}
	turns, err := e.store.ListTurns(threadID)
	if err != nil {
		return err
	}
	drop := make([]string, 0, len(turns))
	for _, t := range turns {
		if t.Seq >= turn.Seq {
			drop = append(drop, t.ID)
		}
	}

	e.recordMu.Lock()
	err = e.store.TruncateFromEventSeq(threadID, fromSeq)
	if err == nil {
		for _, id := range drop {
			e.droppedTurns[id] = struct{}{}
		}
	}
	e.recordMu.Unlock()
	if err != nil {
		return err
	}
	e.emit(store.Event{
		ThreadID: threadID,
		Kind:     KindRewound,
		AgentID:  swarm.DefaultManagerID,
		Text:     strconv.FormatInt(fromSeq, 10),
	})
	return nil
}

func (e *Engine) waitTurnClosed(turnID string, d time.Duration) {
	if turnID == "" {
		return
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		turn, err := e.store.GetTurn(turnID)
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		if err == nil && turn.Status != store.TurnRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *Engine) loadImageInputs(threadID string, refs []store.ImageRef) []ImageInput {
	if len(refs) == 0 {
		return nil
	}
	images := make([]ImageInput, 0, len(refs))
	for _, ref := range refs {
		data, mime, err := e.ReadInputImage(threadID, ref.ID)
		if err != nil {
			e.log.Warn("could not reload a pasted image", "thread", threadID, "image", ref.ID, "err", err)
			continue
		}
		if ref.MIME != "" {
			mime = ref.MIME
		}
		images = append(images, ImageInput{Name: ref.Name, MIME: mime, Data: data})
	}
	return images
}

package engine

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindRewound marks where a conversation was truncated so a user message can
// be resent from that position; its text is the first removed seq. A live
// client needs the cut point to drop the rows that just vanished, and one
// that lagged or reconnected finds it through catch-up because it is stored.
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
		e.recordRewindLocked(threadID, fromSeq)
	}
	e.recordMu.Unlock()
	if err != nil {
		return err
	}
	e.generations.Lock()
	delete(e.generations.threads, threadID)
	e.generations.Unlock()
	e.reindex(threadID)
	return nil
}

// recordRewindLocked stores and broadcasts the cut in the truncation's
// recordMu critical section (modelled in models/stream). A row recorded in
// between would reach readers before the cut and be erased by it; an
// unstored marker is lost to a reader that lags or reconnects; and its seq
// keeps a restarted counter above the removed rows, which a reader may still
// hold as its resume cursor.
func (e *Engine) recordRewindLocked(threadID string, fromSeq int64) {
	marker := store.Event{
		ThreadID: threadID,
		Kind:     KindRewound,
		AgentID:  swarm.DefaultManagerID,
		Text:     strconv.FormatInt(fromSeq, 10),
	}
	if err := e.store.AppendEvent(&marker); err != nil {
		e.log.Warn("could not persist a rewind marker", "thread", threadID, "from", fromSeq, "err", err)
		marker.Seq, marker.CreatedAt = 0, time.Now().UTC()
	}
	e.broadcast(marker)
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

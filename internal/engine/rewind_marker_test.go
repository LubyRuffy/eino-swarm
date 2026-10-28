package engine

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// rewindableThread stores two finished turns directly, so applyRewind can cut
// the second without a running turn or a background title task touching the
// store.
func rewindableThread(t *testing.T, e *Engine) (threadID string, cut, highest int64) {
	t.Helper()
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		turn := &store.Turn{ThreadID: th.ID, UserText: "request"}
		if err := e.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "request"})
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindReasoning, Text: "thinking"})
		if err := e.Store().FinishTurn(turn.ID, store.TurnDone, "answer", ""); err != nil {
			t.Fatal(err)
		}
	}
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil || len(events) != 4 {
		t.Fatalf("seed events=%d err=%v", len(events), err)
	}
	return th.ID, events[2].Seq, events[3].Seq
}

// A desktop that lagged or reconnected resumes with Replay(since=highest).
// When the cut was only broadcast, that reader kept the removed rows on
// screen until a full reload.
func TestALaggedReaderStillFindsTheRewind(t *testing.T) {
	e := newTestEngine(t)
	threadID, cut, highest := rewindableThread(t, e)
	if err := e.applyRewind(threadID, cut); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Replay(threadID, highest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Kind != KindRewound || rows[0].Text != strconv.FormatInt(cut, 10) || rows[0].Seq <= highest {
		t.Fatalf("catch-up after the cut=%+v, want one stored rewound row above seq %d", rows, highest)
	}
	kept, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range kept {
		if row.Seq >= cut && row.Kind != KindRewound {
			t.Fatalf("a removed row survived the cut: %+v", row)
		}
	}
}

// A reader's resume cursor is the highest seq it saw, which can be a row
// the rewind removed. If a restarted store numbered new rows from what
// remained, the reader's `since` would skip the replacement turn.
func TestNumberingAfterRewindAndRestartStaysAboveReaderCursors(t *testing.T) {
	e := newTestEngine(t)
	threadID, cut, highest := rewindableThread(t, e)
	if err := e.applyRewind(threadID, cut); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.Open(e.Config().DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	next := store.Event{ThreadID: threadID, Kind: KindUser, Text: "after restart"}
	if err := restarted.AppendEvent(&next); err != nil {
		t.Fatal(err)
	}
	if next.Seq <= highest {
		t.Fatalf("restarted numbering reused seq %d at or below a reader cursor %d", next.Seq, highest)
	}
}

// Readers apply a cut to everything they hold. A row recorded between the
// truncation and the marker's broadcast would reach them first and then be
// erased by it, so subscribers must see stored seqs strictly increasing.
func TestRewindIsBroadcastInSeqOrderWithConcurrentRows(t *testing.T) {
	e := newTestEngine(t)
	threadID, cut, _ := rewindableThread(t, e)
	sub := e.Subscribe(threadID)
	defer sub.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			e.record(store.Event{ThreadID: threadID, Kind: KindTitle, Text: "concurrent"})
		}
	}()
	if err := e.applyRewind(threadID, cut); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	var last int64
	sawMarker := false
	for done := false; !done; {
		select {
		case ev := <-sub.C:
			if ev.Seq <= last {
				t.Fatalf("stored seq %d (%s) arrived after %d", ev.Seq, ev.Kind, last)
			}
			last, sawMarker = ev.Seq, sawMarker || ev.Kind == KindRewound
		case <-time.After(200 * time.Millisecond):
			done = true
		}
	}
	if !sawMarker {
		t.Fatal("the subscriber never saw the rewind")
	}
}

// A refused marker write must not hide the cut from connected readers, even
// though it cannot be caught up later.
func TestAnUnstoredRewindStillReachesLiveReaders(t *testing.T) {
	e := newTestEngine(t)
	threadID, cut, _ := rewindableThread(t, e)
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("refuse_rewind_marker", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == KindRewound {
			_ = tx.AddError(errors.New("marker refused"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(threadID)
	defer sub.Close()
	if err := e.applyRewind(threadID, cut); err != nil {
		t.Fatal(err)
	}
	saw := waitForKind(t, sub.C, KindRewound, 5*time.Second)
	if saw.Seq != 0 || saw.Text != strconv.FormatInt(cut, 10) || saw.CreatedAt.IsZero() {
		t.Fatalf("unstored marker=%+v", saw)
	}
}

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "zwai.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestThreadCRUD(t *testing.T) {
	s := open(t)

	th := &Thread{Title: "first", ProviderID: "p1"}
	if err := s.CreateThread(th); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if th.ID == "" || th.CreatedAt.IsZero() || th.LastActiveAt.IsZero() {
		t.Fatalf("CreateThread did not fill in the record: %+v", th)
	}

	got, err := s.GetThread(th.ID)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if got.Title != "first" {
		t.Fatalf("title=%q", got.Title)
	}

	if err := s.UpdateThread(th.ID, map[string]any{"title": "renamed"}); err != nil {
		t.Fatalf("UpdateThread: %v", err)
	}
	got, _ = s.GetThread(th.ID)
	if got.Title != "renamed" {
		t.Fatalf("rename did not stick: %q", got.Title)
	}
	if err := s.UpdateThread(th.ID, map[string]any{"model": "alpha"}); err != nil {
		t.Fatalf("UpdateThread model: %v", err)
	}
	got, _ = s.GetThread(th.ID)
	if got.Model != "alpha" {
		t.Fatalf("conversation model did not stick: %q", got.Model)
	}
	now := time.Now().UTC()
	if err := s.UpdateThread(th.ID, map[string]any{"pinned": true, "pinned_at": now}); err != nil {
		t.Fatalf("UpdateThread pin: %v", err)
	}
	got, _ = s.GetThread(th.ID)
	if !got.Pinned || got.PinnedAt == nil {
		t.Fatalf("pin did not stick: %+v", got)
	}
	if err := s.UpdateThread(th.ID, map[string]any{"pinned": false, "pinned_at": nil}); err != nil {
		t.Fatalf("UpdateThread unpin: %v", err)
	}
	got, _ = s.GetThread(th.ID)
	if got.Pinned || got.PinnedAt != nil {
		t.Fatalf("unpin did not clear: %+v", got)
	}
	if err := s.UpdateThread(th.ID, nil); err != nil {
		t.Fatalf("an empty patch is a no-op, not an error: %v", err)
	}
	if err := s.UpdateThread("missing", map[string]any{"title": "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.GetThread("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// archived threads stay out of the sidebar unless asked for
	arch := &Thread{Title: "old", Archived: true}
	if err := s.CreateThread(arch); err != nil {
		t.Fatal(err)
	}
	active, err := s.ListThreads(false, "")
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(active) != 1 || active[0].ID != th.ID {
		t.Fatalf("archived thread leaked into the active list: %+v", active)
	}
	all, _ := s.ListThreads(true, "")
	if len(all) != 2 {
		t.Fatalf("want 2 threads, got %d", len(all))
	}
}

// ApplyAutoTitle is a compare-and-swap: a user rename in between must win,
// otherwise a slow namer puts the generated name back over what they typed.
func TestApplyAutoTitleOnlyWhileMachineOwned(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "placeholder", TitleAuto: true, ProviderID: "p1"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	ok, err := s.ApplyAutoTitle(th.ID, " Generated name ")
	if err != nil || !ok {
		t.Fatalf("ApplyAutoTitle: ok=%v err=%v", ok, err)
	}
	got, _ := s.GetThread(th.ID)
	if got.Title != "Generated name" || got.TitleAuto {
		t.Fatalf("landed title=%+v", got)
	}
	ok, err = s.ApplyAutoTitle(th.ID, "second try")
	if err != nil || ok {
		t.Fatalf("a second apply must not overwrite: ok=%v err=%v", ok, err)
	}
	got, _ = s.GetThread(th.ID)
	if got.Title != "Generated name" {
		t.Fatalf("second apply overwrote: %q", got.Title)
	}

	owned := &Thread{Title: "placeholder", TitleAuto: true, ProviderID: "p1"}
	if err := s.CreateThread(owned); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(owned.ID, map[string]any{"title": "Mine", "title_auto": false}); err != nil {
		t.Fatal(err)
	}
	ok, err = s.ApplyAutoTitle(owned.ID, "generated")
	if err != nil || ok {
		t.Fatalf("rename must beat the namer: ok=%v err=%v", ok, err)
	}
	got, _ = s.GetThread(owned.ID)
	if got.Title != "Mine" {
		t.Fatalf("namer overwrote a rename: %q", got.Title)
	}
	ok, err = s.ApplyAutoTitle("missing", "x")
	if err != nil || ok {
		t.Fatalf("missing id: ok=%v err=%v", ok, err)
	}
	ok, err = s.ApplyAutoTitle(th.ID, "  ")
	if err != nil || ok {
		t.Fatalf("blank title: ok=%v err=%v", ok, err)
	}
}

func TestListThreadsOrdersByActivity(t *testing.T) {
	s := open(t)
	older := &Thread{Title: "older"}
	newer := &Thread{Title: "newer"}
	if err := s.CreateThread(older); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(newer); err != nil {
		t.Fatal(err)
	}
	// touching the older one must float it to the top of the sidebar
	if err := s.TouchThread(older.ID); err != nil {
		t.Fatalf("TouchThread: %v", err)
	}
	list, err := s.ListThreads(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != older.ID {
		t.Fatalf("want the touched thread first, got %+v", list)
	}
}

func TestDeleteThreadRemovesEverythingAttached(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "doomed"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, UserText: "hi"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "delta", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: turn.ID, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAttachment(&Attachment{ThreadID: th.ID, Name: "a.txt", RelPath: "a.txt"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if msgs, _ := s.ListMessages(th.ID); len(msgs) != 0 {
		t.Fatalf("messages survived: %+v", msgs)
	}
	if evts, _ := s.ListEvents(th.ID, 0, 0); len(evts) != 0 {
		t.Fatalf("events survived: %+v", evts)
	}
	if turns, _ := s.ListTurns(th.ID); len(turns) != 0 {
		t.Fatalf("turns survived: %+v", turns)
	}
	if calls, _ := s.ListLLMCalls(turn.ID); len(calls) != 0 {
		t.Fatalf("llm calls survived: %+v", calls)
	}
	if atts, _ := s.ListAttachments(th.ID); len(atts) != 0 {
		t.Fatalf("attachments survived: %+v", atts)
	}
	if err := s.DeleteThread(th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound on a second delete, got %v", err)
	}
}

func TestEventSequenceIsGapFreeAndReplayable(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		e := &Event{ThreadID: th.ID, Kind: "delta", Text: strings.Repeat("x", i+1)}
		if err := s.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d got seq %d", i, e.Seq)
		}
	}

	// a reconnecting client asks for everything after what it already has
	tail, err := s.ListEvents(th.ID, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].Seq != 4 || tail[1].Seq != 5 {
		t.Fatalf("replay from seq 3 wrong: %+v", tail)
	}
	if limited, _ := s.ListEvents(th.ID, 0, 2); len(limited) != 2 {
		t.Fatalf("limit ignored: %d", len(limited))
	}
}

func TestListTailEventsPagesFromTheEnd(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := s.AppendEvent(&Event{ThreadID: th.ID, Kind: "delta", Text: strings.Repeat("x", i+1)}); err != nil {
			t.Fatal(err)
		}
	}

	page, hasMore, err := s.ListTailEvents(th.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Fatal("older rows must still exist")
	}
	if len(page) != 2 || page[0].Seq != 4 || page[1].Seq != 5 {
		t.Fatalf("tail page=%+v", page)
	}

	older, hasMore, err := s.ListTailEvents(th.ID, page[0].Seq, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(older) != 2 || older[0].Seq != 2 || older[1].Seq != 3 {
		t.Fatalf("older page hasMore=%v %+v", hasMore, older)
	}

	rest, hasMore, err := s.ListTailEvents(th.ID, older[0].Seq, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore || len(rest) != 1 || rest[0].Seq != 1 {
		t.Fatalf("first page hasMore=%v %+v", hasMore, rest)
	}

	tagged := &Thread{Title: "tagged"}
	if err := s.CreateThread(tagged); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: tagged.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: tagged.ID, TurnID: turn.ID, Kind: "user_message", Text: "in"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: tagged.ID, Kind: "user_message", Text: "out"}); err != nil {
		t.Fatal(err)
	}
	byTurn, err := s.ListEventsByTurn(tagged.ID, turn.ID)
	if err != nil || len(byTurn) != 1 || byTurn[0].Text != "in" {
		t.Fatalf("by turn %v %+v", err, byTurn)
	}
	missing, err := s.ListEventsByTurn(tagged.ID, "tn_missing")
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing turn %v %+v", err, missing)
	}
	turnTail, moreTurn, err := s.ListTailEventsByTurn(tagged.ID, turn.ID, 1)
	if err != nil || moreTurn || len(turnTail) != 1 || turnTail[0].Text != "in" {
		t.Fatalf("turn tail %v more=%v %+v", err, moreTurn, turnTail)
	}

	empty, hasMore, err := s.ListTailEvents(th.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore || len(empty) != 0 {
		t.Fatalf("nothing before seq 1: hasMore=%v %+v", hasMore, empty)
	}

	none, hasMore, err := s.ListTailEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore || len(none) != 0 {
		t.Fatalf("limit 0 is empty, not the whole log: hasMore=%v n=%d", hasMore, len(none))
	}
}

func TestListRosterEventsSkipsTheToolRows(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	kinds := []string{
		"user_message", "spawned", "tool_call", "tool_result", "finished", "cleanup", "done",
	}
	for _, kind := range kinds {
		if err := s.AppendEvent(&Event{ThreadID: th.ID, Kind: kind, AgentID: "worker-1"}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListRosterEvents(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("roster=%+v", got)
	}
	if got[0].Kind != "spawned" || got[0].Seq != 2 {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].Kind != "finished" || got[1].Seq != 5 {
		t.Fatalf("second=%+v", got[1])
	}
	if got[2].Kind != "cleanup" || got[2].Seq != 6 {
		t.Fatalf("third=%+v", got[2])
	}

	empty, err := s.ListRosterEvents("th_missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing thread must be empty, not an error: %+v", empty)
	}
}

func TestListAgentEventsIsThatWorkerOnly(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	rows := []Event{
		{ThreadID: th.ID, Kind: "user_message", AgentID: "manager", Text: "go"},
		{ThreadID: th.ID, Kind: "spawned", AgentID: "worker-1", Role: "worker"},
		{ThreadID: th.ID, Kind: "tool_call", AgentID: "worker-1", Text: "exec", ToolCallID: "c1"},
		{ThreadID: th.ID, Kind: "tool_call", AgentID: "worker-2", Text: "exec", ToolCallID: "c2"},
		{ThreadID: th.ID, Kind: "finished", AgentID: "worker-1", Text: "done"},
	}
	for i := range rows {
		if err := s.AppendEvent(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListAgentEvents(th.ID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("worker-1=%+v", got)
	}
	if got[0].Kind != "spawned" || got[1].Kind != "tool_call" || got[2].Kind != "finished" {
		t.Fatalf("kinds=%+v", got)
	}

	none, err := s.ListAgentEvents(th.ID, "worker-missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("missing worker must be empty: %+v", none)
	}
	blank, err := s.ListAgentEvents(th.ID, "")
	if err != nil || len(blank) != 0 {
		t.Fatalf("blank agent: err=%v %+v", err, blank)
	}
}

func TestConcurrentEventWritesDoNotBusyTheDatabase(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	const n = 40
	errs := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := s.AppendEvent(&Event{
				ThreadID: th.ID, Kind: "delta", Text: strings.Repeat("x", i+1),
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}
	events, err := s.ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}
}

func TestSequencesResumeAfterReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "zwai.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "t"}
	if err := s1.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s1.AppendEvent(&Event{ThreadID: th.ID, Kind: "delta"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s1.AppendMessages(th.ID, "tn_1", []Message{{Role: "user", Content: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := s1.CreateTurn(&Turn{ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	e := &Event{ThreadID: th.ID, Kind: "delta"}
	if err := s2.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	if e.Seq != 4 {
		t.Fatalf("event seq restarted at %d instead of continuing at 4", e.Seq)
	}
	msgs := []Message{{Role: "assistant", Content: "b"}}
	if err := s2.AppendMessages(th.ID, "tn_2", msgs); err != nil {
		t.Fatal(err)
	}
	stored, _ := s2.ListMessages(th.ID)
	if len(stored) != 2 || stored[1].Seq != 2 {
		t.Fatalf("message seq did not continue: %+v", stored)
	}
	tn := &Turn{ThreadID: th.ID}
	if err := s2.CreateTurn(tn); err != nil {
		t.Fatal(err)
	}
	if tn.Seq != 2 {
		t.Fatalf("turn seq did not continue: %d", tn.Seq)
	}
}

func TestConcurrentEventAppendsAreUnique(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}

	const n = 60
	var wg sync.WaitGroup
	seqs := make([]int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := &Event{ThreadID: th.ID, Kind: "delta"}
			if err := s.AppendEvent(e); err != nil {
				t.Errorf("AppendEvent: %v", err)
				return
			}
			seqs[i] = e.Seq
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for _, q := range seqs {
		if q == 0 {
			t.Fatal("an event got no sequence number")
		}
		if seen[q] {
			t.Fatalf("duplicate sequence number %d: a reconnecting client would skip an event", q)
		}
		seen[q] = true
	}
	if len(seen) != n {
		t.Fatalf("want %d distinct sequences, got %d", n, len(seen))
	}
}

func TestTurnLifecycle(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}

	turn := &Turn{ThreadID: th.ID, UserText: "do it", ProviderID: "p", Model: "m"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.Status != TurnRunning || turn.StartedAt.IsZero() || turn.Seq != 1 {
		t.Fatalf("CreateTurn defaults wrong: %+v", turn)
	}

	if err := s.FinishTurn(turn.ID, TurnDone, "the answer", ""); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	got, err := s.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != TurnDone || got.Final != "the answer" || got.EndedAt == nil {
		t.Fatalf("turn not closed properly: %+v", got)
	}
	if got.DurationMS < 0 {
		t.Fatalf("negative duration: %d", got.DurationMS)
	}
	if _, err := s.GetTurn("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.FinishTurn("missing", TurnDone, "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// Startup asks for every turn still marked running so it can continue them
// rather than pretending the user stopped them.
func TestListRunningTurns(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	running := &Turn{ThreadID: th.ID, UserText: "still going"}
	if err := s.CreateTurn(running); err != nil {
		t.Fatal(err)
	}
	done := &Turn{ThreadID: th.ID, UserText: "finished"}
	if err := s.CreateTurn(done); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(done.ID, TurnDone, "x", ""); err != nil {
		t.Fatal(err)
	}
	cancelled := &Turn{ThreadID: th.ID, UserText: "stopped"}
	if err := s.CreateTurn(cancelled); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(cancelled.ID, TurnCancelled, "", "interrupted"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListRunningTurns()
	if err != nil {
		t.Fatalf("ListRunningTurns: %v", err)
	}
	if len(got) != 1 || got[0].ID != running.ID || got[0].Status != TurnRunning {
		t.Fatalf("want the one unfinished turn, got %+v", got)
	}
}

// A crash leaves turns marked running. The method still exists as a bulk
// cancel; startup no longer uses it — it resumes those turns instead.
func TestMarkStaleTurnsCancelled(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	running := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(running); err != nil {
		t.Fatal(err)
	}
	closed := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(closed); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(closed.ID, TurnDone, "x", ""); err != nil {
		t.Fatal(err)
	}

	n, err := s.MarkStaleTurnsCancelled()
	if err != nil {
		t.Fatalf("MarkStaleTurnsCancelled: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 stale turn closed, got %d", n)
	}
	got, _ := s.GetTurn(running.ID)
	if got.Status != TurnCancelled || got.Error == "" {
		t.Fatalf("stale turn not closed: %+v", got)
	}
	stillDone, _ := s.GetTurn(closed.ID)
	if stillDone.Status != TurnDone {
		t.Fatalf("a finished turn was rewritten: %+v", stillDone)
	}
}

func TestMessagesAndTraceQueries(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendMessages(th.ID, turn.ID, nil); err != nil {
		t.Fatalf("appending nothing is a no-op: %v", err)
	}
	msgs := []Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "", ToolCalls: `[{"id":"c1","function":{"name":"read"}}]`},
		{Role: "tool", Content: "file body", ToolCallID: "c1"},
		{Role: "assistant", Content: "answer"},
	}
	if err := s.AppendMessages(th.ID, turn.ID, msgs); err != nil {
		t.Fatal(err)
	}
	stored, err := s.ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4 {
		t.Fatalf("want 4 messages, got %d", len(stored))
	}
	for i := range stored {
		if stored[i].Seq != int64(i+1) {
			t.Fatalf("message %d out of order: seq=%d", i, stored[i].Seq)
		}
		if stored[i].ThreadID != th.ID || stored[i].TurnID != turn.ID {
			t.Fatalf("message %d not linked: %+v", i, stored[i])
		}
	}
	if stored[2].ToolCallID != "c1" {
		t.Fatalf("tool result lost its call id: %+v", stored[2])
	}

	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "[steer] drop me", EventSeq: 42},
		{Role: "user", Content: "[steer] keep me", EventSeq: 43},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMessageByEventSeq(th.ID, 42); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var captions []string
	for _, m := range after {
		if strings.HasPrefix(m.Content, "[steer]") {
			captions = append(captions, m.Content)
		}
	}
	if len(captions) != 1 || captions[0] != "[steer] keep me" {
		t.Fatalf("retract dropped the wrong steer: %q", captions)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "[steer] legacy", EventSeq: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSteerMessage(th.ID, 99, "legacy"); err != nil {
		t.Fatal(err)
	}
	afterLegacy, err := s.ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range afterLegacy {
		if m.Content == "[steer] legacy" {
			t.Fatal("event_seq 0 steer survived DeleteSteerMessage")
		}
	}

	// the trace view: one turn's events plus its model calls
	for i := 0; i < 3; i++ {
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "tool_call"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: "other", Kind: "delta"}); err != nil {
		t.Fatal(err)
	}
	tEvents, err := s.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tEvents) != 3 {
		t.Fatalf("turn events leaked across turns: %d", len(tEvents))
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: turn.ID, Model: "m", InputMsgs: 4, DurationMS: 12}); err != nil {
		t.Fatal(err)
	}
	calls, err := s.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].InputMsgs != 4 {
		t.Fatalf("llm call not recorded: %+v", calls)
	}
}

func TestAttachments(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := s.AddAttachment(&Attachment{ThreadID: th.ID, Name: name, RelPath: name, Size: 3}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListAttachments(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "b.txt" {
		t.Fatalf("want newest first, got %+v", list)
	}
	if err := s.DeleteAttachmentByPath(th.ID, "a.txt"); err != nil {
		t.Fatalf("DeleteAttachmentByPath: %v", err)
	}
	list, _ = s.ListAttachments(th.ID)
	if len(list) != 1 || list[0].Name != "b.txt" {
		t.Fatalf("wrong attachment removed: %+v", list)
	}
}

func TestBindAttachmentTurn(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	att := &Attachment{ThreadID: th.ID, Name: "a.csv", RelPath: "uploads/a.csv", Size: 3}
	if err := s.AddAttachment(att); err != nil {
		t.Fatal(err)
	}
	if err := s.BindAttachmentTurn(th.ID, att.RelPath, "tn_1"); err != nil {
		t.Fatalf("BindAttachmentTurn: %v", err)
	}
	list, err := s.ListAttachments(th.ID)
	if err != nil || len(list) != 1 || list[0].TurnID != "tn_1" {
		t.Fatalf("turn not bound: %+v %v", list, err)
	}
	if err := s.BindAttachmentTurn(th.ID, "uploads/missing.csv", "tn_1"); err == nil {
		t.Fatal("binding a path that was never uploaded must fail")
	}
}

func TestPastedImageRefsRoundTrip(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	refs := []ImageRef{{ID: "img_ab12", Name: "clip.png", MIME: "image/png"}}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "look", Images: refs},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{
		ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "look", Images: refs,
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.ListMessages(th.ID)
	if err != nil || len(msgs) != 1 || len(msgs[0].Images) != 1 || msgs[0].Images[0].ID != "img_ab12" {
		t.Fatalf("message images did not round-trip: %+v %v", msgs, err)
	}
	evts, err := s.ListEvents(th.ID, 0, 0)
	if err != nil || len(evts) != 1 || len(evts[0].Images) != 1 || evts[0].Images[0].MIME != "image/png" {
		t.Fatalf("event images did not round-trip: %+v %v", evts, err)
	}
}

func TestOpenInMemoryAndDBHandle(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Fatal("DB handle must be exposed for ad-hoc queries")
	}
	if err := s.CreateThread(&Thread{Title: "x"}); err != nil {
		t.Fatalf("in-memory store must be usable: %v", err)
	}
}

func TestOpenRejectsUnusablePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := writeFile(file); err != nil {
		t.Fatal(err)
	}
	// a path whose parent is a regular file cannot be created
	if _, err := Open(filepath.Join(file, "nested", "zwai.db")); err == nil {
		t.Fatal("want an error for an unusable database path")
	}
}

// When the database goes away mid-session (disk unmounted, file deleted,
// process shutting down) every call must report an error rather than panic or
// silently claim success.
func TestClosedStoreReportsErrorsEverywhere(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zwai.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	checks := map[string]func() error{
		"CreateThread":     func() error { return s.CreateThread(&Thread{Title: "x"}) },
		"GetThread":        func() error { _, e := s.GetThread(th.ID); return e },
		"ListThreads":      func() error { _, e := s.ListThreads(false, ""); return e },
		"UpdateThread":     func() error { return s.UpdateThread(th.ID, map[string]any{"title": "x"}) },
		"DeleteThread":     func() error { return s.DeleteThread(th.ID) },
		"AppendMessages":   func() error { return s.AppendMessages(th.ID, turn.ID, []Message{{Role: "user"}}) },
		"ListMessages":     func() error { _, e := s.ListMessages(th.ID); return e },
		"CreateTurn":       func() error { return s.CreateTurn(&Turn{ThreadID: th.ID}) },
		"FinishTurn":       func() error { return s.FinishTurn(turn.ID, TurnDone, "", "") },
		"GetTurn":          func() error { _, e := s.GetTurn(turn.ID); return e },
		"ListTurns":        func() error { _, e := s.ListTurns(th.ID); return e },
		"ListRunningTurns": func() error { _, e := s.ListRunningTurns(); return e },
		"MarkStaleTurnsCancelled": func() error {
			_, e := s.MarkStaleTurnsCancelled()
			return e
		},
		"AppendEvent":            func() error { return s.AppendEvent(&Event{ThreadID: th.ID, Kind: "delta"}) },
		"ListEvents":             func() error { _, e := s.ListEvents(th.ID, 0, 0); return e },
		"ListTailEvents":         func() error { _, _, e := s.ListTailEvents(th.ID, 0, 2); return e },
		"ListRosterEvents":       func() error { _, e := s.ListRosterEvents(th.ID); return e },
		"ListAgentEvents":        func() error { _, e := s.ListAgentEvents(th.ID, "worker-1"); return e },
		"ListTurnEvents":         func() error { _, e := s.ListTurnEvents(turn.ID); return e },
		"AppendLLMCall":          func() error { return s.AppendLLMCall(&LLMCall{ThreadID: th.ID}) },
		"ListLLMCalls":           func() error { _, e := s.ListLLMCalls(turn.ID); return e },
		"SummarizeUsage":         func() error { _, e := s.SummarizeUsage(th.ID, turn.ID); return e },
		"AddAttachment":          func() error { return s.AddAttachment(&Attachment{ThreadID: th.ID, Name: "a"}) },
		"ListAttachments":        func() error { _, e := s.ListAttachments(th.ID); return e },
		"DeleteAttachmentByPath": func() error { return s.DeleteAttachmentByPath(th.ID, "a") },
		"CreateProject":          func() error { return s.CreateProject(&Project{Name: "p"}) },
		"GetProject":             func() error { _, e := s.GetProject("pj_1"); return e },
		"ListProjects":           func() error { _, e := s.ListProjects(); return e },
		"UpdateProject":          func() error { return s.UpdateProject("pj_1", map[string]any{"name": "x"}) },
		"DeleteProject":          func() error { return s.DeleteProject("pj_1") },
		"ListThreadIDsByProject": func() error { _, e := s.ListThreadIDsByProject("pj_1"); return e },
		"ReorderThreads":         func() error { return s.ReorderThreads([]string{th.ID}) },
		"ReorderProjects":        func() error { return s.ReorderProjects([]string{"pj_1"}) },
		"TouchThread":            func() error { return s.TouchThread(th.ID) },
	}
	for name, fn := range checks {
		if err := fn(); err == nil {
			t.Errorf("%s returned no error on a closed database", name)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing twice must stay harmless: %v", err)
	}
}

func TestNewIDIsUniqueAndPathSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := NewID("th_")
		if !strings.HasPrefix(id, "th_") {
			t.Fatalf("missing prefix: %q", id)
		}
		if strings.ContainsAny(id, "/\\.: *?\"<>|") {
			t.Fatalf("id is not filesystem safe: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestApplyTurnUpdateMissingTurnIsNotFound(t *testing.T) {
	s := open(t)
	if err := s.applyTurnUpdate("tu_missing", map[string]any{"status": TurnDone}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpdateTurnMarksAQuietScheduledRow(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, UserText: "Continue the wait."}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTurn(turn.ID, nil); err != nil {
		t.Fatalf("empty patch is a no-op: %v", err)
	}
	got, err := s.GetTurn(turn.ID)
	if err != nil || got.Quiet {
		t.Fatalf("empty patch must not stamp quiet: %+v err=%v", got, err)
	}
	if err := s.UpdateTurn(turn.ID, map[string]any{"quiet": true}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTurn(turn.ID)
	if err != nil || !got.Quiet {
		t.Fatalf("quiet=%v err=%v", got, err)
	}
	if err := s.UpdateTurn("tu_missing", map[string]any{"quiet": true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func writeFile(p string) error {
	return os.WriteFile(p, []byte("x"), 0o600)
}

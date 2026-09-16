package server_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestStartTurnRewindsFromAUserMessage(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the first request"}, http.StatusAccepted)
	h.waitTurnDone(id)
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the second request"}, http.StatusAccepted)
	h.waitTurnDone(id)

	seq := firstUserSeq(t, h.app.Store, id)
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the edited request", "from_event_seq": seq},
		http.StatusAccepted)
	h.waitTurnDone(id)

	turns, err := h.app.Store.ListTurns(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("want only the replacement turn, got %d", len(turns))
	}
	events, err := h.app.Store.ListEvents(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == engine.KindRewound {
			t.Fatal("rewound is live-only; storing it would replay a cut that already happened")
		}
		if ev.Kind == engine.KindUser && ev.Text != "the edited request" {
			t.Fatalf("a dropped request is still on the timeline: %q", ev.Text)
		}
	}
}

func TestStartTurnRewindRejectsANonUserEvent(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the first request"}, http.StatusAccepted)
	h.waitTurnDone(id)
	events, err := h.app.Store.ListEvents(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var other int64
	for _, ev := range events {
		if ev.Kind != engine.KindUser {
			other = ev.Seq
			break
		}
	}
	if other == 0 {
		t.Fatal("need a non-user event to refuse")
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "edited", "from_event_seq": other},
		http.StatusBadRequest)
}

func TestStartTurnRewindIsNotBusy(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the original request"}, http.StatusAccepted)
	seq := waitUserSeq(t, h.app.Store, id)
	got := h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the replacement request", "from_event_seq": seq},
		http.StatusAccepted)
	if got["turn"] == nil {
		t.Fatalf("a rewind of a running turn must start, not 409: %v", got)
	}
}

func TestSteerIgnoresFromEventSeq(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the first request"}, http.StatusAccepted)
	h.waitTurnDone(id)
	seq := firstUserSeq(t, h.app.Store, id)
	h.json(http.MethodPost, "/api/threads/"+id+"/steer",
		map[string]any{"text": "the next request", "from_event_seq": seq},
		http.StatusAccepted)
	h.waitTurnDone(id)
	turns, err := h.app.Store.ListTurns(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("steer must not rewind; want 2 turns, got %d", len(turns))
	}
}

func firstUserSeq(t *testing.T, s *store.Store, threadID string) int64 {
	t.Helper()
	events, err := s.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == engine.KindUser {
			return ev.Seq
		}
	}
	t.Fatal("no user_message")
	return 0
}

func waitUserSeq(t *testing.T, s *store.Store, threadID string) int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if seq := peekUserSeq(s, threadID); seq > 0 {
			return seq
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("user_message never arrived")
	return 0
}

func peekUserSeq(s *store.Store, threadID string) int64 {
	events, err := s.ListEvents(threadID, 0, 0)
	if err != nil {
		return 0
	}
	for _, ev := range events {
		if ev.Kind == engine.KindUser {
			return ev.Seq
		}
	}
	return 0
}

package remote

import (
	"encoding/json"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func completionResponse(t *testing.T, r Response) map[string]any {
	t.Helper()
	if !r.OK {
		t.Fatalf("RPC rejected: %+v", r)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Attention is independent of the live roster and the five-row idle page.
// Otherwise a task completed outside page one vanishes before it is viewed.
func TestCompletionAttentionIncludesEveryUnreadConversationWithoutRunningIt(t *testing.T) {
	e := testEngine(t)
	var firstID string
	var firstSeq int64
	for i := 0; i < 7; i++ {
		th, err := e.CreateThread("completed", "", "")
		if err != nil {
			t.Fatal(err)
		}
		turn := &store.Turn{ThreadID: th.ID}
		if err := e.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		ev := &store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: "done"}
		if err := e.Store().AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstID, firstSeq = th.ID, ev.Seq
		}
	}
	cfg := config.RemoteConfig{ThreadLimit: 1}
	resp := Handle(e, cfg, Request{Op: OpList}, "direct", "session")
	got := completionResponse(t, resp)
	attention, ok := got["attention"].([]any)
	if !ok || len(attention) != 7 {
		t.Fatalf("unread completion clipped by idle page: %+v", got)
	}
	if len(resp.Running) != 0 || len(resp.Threads) != 1 {
		t.Fatalf("attention changed live/paging semantics: %+v", resp)
	}
	opened := completionResponse(t, Handle(e, cfg, Request{Op: OpOpen, ThreadID: firstID}, "direct", "session"))
	detail := opened["detail"].(map[string]any)
	if detail["unread"] != true || detail["completed_seq"] != float64(firstSeq) {
		t.Fatalf("open lost read identity: %+v", detail)
	}
	var req Request
	if err := json.Unmarshal([]byte(`{"op":"read","thread_id":"`+firstID+`","completed_seq":1}`), &req); err != nil {
		t.Fatal(err)
	}
	completionResponse(t, Handle(e, cfg, req, "direct", "session"))
	after := completionResponse(t, Handle(e, cfg, Request{Op: OpList}, "direct", "session"))
	if len(after["attention"].([]any)) != 6 {
		t.Fatalf("exact read did not remove just its conversation: %+v", after)
	}
}

func TestCompletionReadRPCPreservesErrorCodes(t *testing.T) {
	e := testEngine(t)
	for _, tc := range []struct{ body, code string }{
		{`{"op":"read","thread_id":"missing","completed_seq":1}`, "not_found"},
		{`{"op":"read","thread_id":"missing","completed_seq":-1}`, "bad_request"},
	} {
		var req Request
		if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
			t.Fatal(err)
		}
		r := Handle(e, config.RemoteConfig{}, req, "direct", "session")
		if r.OK || r.Code != tc.code {
			t.Fatalf("read error %+v, want %s", r, tc.code)
		}
	}
}

func TestCompletionReadThroughLinkDispatchReturnsCurrentUnreadDetail(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("completed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	first := &store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done", AgentID: swarm.DefaultManagerID}
	if err := e.Store().AppendEvent(first); err != nil {
		t.Fatal(err)
	}
	second := &store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "error", AgentID: swarm.DefaultManagerID}
	if err := e.Store().AppendEvent(second); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	for _, tc := range []struct {
		id     string
		seq    int64
		unread bool
	}{{"old", first.Seq, true}, {"current", second.Seq, false}, {"retry", first.Seq, false}} {
		raw, err := json.Marshal(Request{V: ProtocolV, ID: tc.id, Op: OpRead, ThreadID: th.ID, CompletedSeq: tc.seq})
		if err != nil {
			t.Fatal(err)
		}
		pump.Dispatch(raw)
		resp := log.waitID(t, tc.id, 2*time.Second)
		if !resp.OK || resp.Detail == nil || resp.Detail.ID != th.ID ||
			resp.Detail.CompletedSeq != second.Seq || resp.Detail.Unread != tc.unread {
			t.Fatalf("link read %s lost current identity: %+v", tc.id, resp)
		}
	}
}

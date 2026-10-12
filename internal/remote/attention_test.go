package remote

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/protocol"
)

func TestCompletionAttentionDoesNotMakeListExceedSealedFrame(t *testing.T) {
	e := testEngine(t)
	for i := 0; i < 80; i++ {
		th, err := e.CreateThread(strings.Repeat("界", 200), "", "")
		if err != nil {
			t.Fatal(err)
		}
		turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone, Final: strings.Repeat("果", 200)}
		if err := e.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	resp := Handle(e, config.RemoteConfig{ThreadLimit: 1}, Request{ID: "list", Op: OpList}, "direct", "session")
	seen := make(map[string]bool)
	pages := 0
	for {
		assertAttentionFrame(t, resp)
		pages++
		for _, row := range resp.Attention {
			if !row.Unread || seen[row.ID] {
				t.Fatalf("page repeated or included a read task: %+v", row)
			}
			seen[row.ID] = true
		}
		if !resp.AttentionMore {
			break
		}
		if len(resp.Attention) == 0 || resp.AttentionCursor == "" {
			t.Fatal("pagination cannot advance")
		}
		if pages == 1 {
			// The cursor row may be read before More. A keyset must still
			// reach the other unseen rows instead of starting page one again.
			last := resp.Attention[len(resp.Attention)-1]
			if err := e.Store().MarkThreadRead(last.ID, last.CompletedSeq); err != nil {
				t.Fatal(err)
			}
		}
		resp = Handle(e, config.RemoteConfig{ThreadLimit: 1}, Request{ID: "more", Op: OpMore,
			Group: GroupAttention, Cursor: resp.AttentionCursor}, "direct", "session")
		if len(resp.Threads) != 0 || len(resp.Running) != 0 || len(resp.Groups) != 0 {
			t.Fatal("attention More replaced an unrelated inbox section")
		}
	}
	if len(seen) != 80 || pages < 2 {
		t.Fatalf("attention silently clipped: %d tasks across %d pages", len(seen), pages)
	}
}

func assertAttentionFrame(t *testing.T, resp Response) {
	t.Helper()
	raw, err := json.Marshal(resp)
	if err != nil || !resp.OK {
		t.Fatalf("attention reply: code=%s %v", resp.Code, err)
	}
	// The actual frame encoder includes Seal's counter/tag overhead.
	_, err = (protocol.Frame{Payload: append(raw, make([]byte, 24)...)}).Marshal()
	if err != nil || len(raw) > plaintextBudget() {
		t.Fatalf("unread list cannot leave the PC: json=%d budget=%d frame=%v", len(raw), plaintextBudget(), err)
	}
}

func TestCompletionAttentionCursorAndOversizeFailuresAreExplicit(t *testing.T) {
	e := testEngine(t)
	for _, cursor := range []string{"!", encodeCursor("not json"), encodeCursor(`{}`)} {
		resp := Handle(e, config.RemoteConfig{}, Request{Op: OpMore, Group: GroupAttention, Cursor: cursor}, "direct", "session")
		if resp.OK || resp.Code != "bad_request" {
			t.Fatalf("invalid cursor accepted: %+v", resp)
		}
	}
	th, err := e.CreateThread("task", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Final: strings.Repeat("界", 100)}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}); err != nil {
		t.Fatal(err)
	}
	base := okBase("list", "direct", "session")
	base.Threads = []ThreadView{{}}
	metadata, _ := json.Marshal(base)
	base.Threads[0].Summary = strings.Repeat("x", plaintextBudget()-len(metadata)-100)
	resp := packAttention(e, config.RemoteConfig{}, Request{Op: OpList}, base)
	assertAttentionFrame(t, resp)
	if len(resp.Attention) != 0 || !resp.AttentionMore || resp.AttentionCursor != "first" {
		t.Fatalf("full metadata hid attention instead of allowing More: %+v", resp)
	}
	page := Handle(e, config.RemoteConfig{}, Request{Op: OpMore, Group: GroupAttention, Cursor: "first"}, "direct", "session")
	assertAttentionFrame(t, page)
	if len(page.Attention) != 1 {
		t.Fatalf("dedicated page failed: %+v", page)
	}
	base.Threads[0].Summary = strings.Repeat("x", plaintextBudget()-len(metadata))
	resp = packAttention(e, config.RemoteConfig{}, Request{Op: OpList}, base)
	if resp.OK || resp.Code != "too_large" {
		t.Fatalf("More metadata itself exceeded the budget: code=%s", resp.Code)
	}
	base.Threads[0].Summary = strings.Repeat("x", plaintextBudget())
	resp = packAttention(e, config.RemoteConfig{}, Request{Op: OpList}, base)
	if resp.OK || resp.Code != "too_large" {
		t.Fatalf("oversized metadata hidden: %+v", resp)
	}
	if err := e.Store().FinishTurn(turn.ID, store.TurnDone, strings.Repeat("x", plaintextBudget()+1), ""); err != nil {
		t.Fatal(err)
	}
	resp = Handle(e, config.RemoteConfig{SummaryChars: plaintextBudget() + 1}, Request{Op: OpMore, Group: GroupAttention}, "direct", "session")
	if resp.OK || resp.Code != "too_large" {
		t.Fatalf("single oversized result hidden: %+v", resp)
	}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	resp = Handle(e, config.RemoteConfig{}, Request{Op: OpMore, Group: GroupAttention}, "direct", "session")
	if resp.OK {
		t.Fatal("closed store became empty attention")
	}
}

func TestCompletionAttentionKeysetHandlesEqualTimestampsAndEnd(t *testing.T) {
	at := time.Now().UTC()
	rows := []store.Thread{{ID: "c", LastActiveAt: at}, {ID: "b", LastActiveAt: at}, {ID: "a", LastActiveAt: at}}
	raw, _ := json.Marshal(attentionCursor{At: at, ID: "b"})
	start, err := attentionStart(rows, encodeCursor(string(raw)))
	if err != nil || start != 2 {
		t.Fatalf("same timestamp skipped older IDs: %d %v", start, err)
	}
	raw, _ = json.Marshal(attentionCursor{At: at.Add(-time.Second), ID: "old"})
	start, err = attentionStart(rows, encodeCursor(string(raw)))
	if err != nil || start != len(rows) {
		t.Fatalf("end cursor restarted: %d %v", start, err)
	}
}

func TestCompletionAttentionEmptyMetadataFailureIsExplicit(t *testing.T) {
	e := testEngine(t)
	end, _ := json.Marshal(attentionCursor{At: time.Now().UTC(), ID: "end"})
	for _, cursor := range []string{"", encodeCursor(string(end))} {
		base := okBase("list", "direct", "session")
		base.Threads = []ThreadView{{Summary: strings.Repeat("x", plaintextBudget())}}
		resp := packAttention(e, config.RemoteConfig{}, Request{ID: "list", Op: OpList, Cursor: cursor}, base)
		raw, _ := json.Marshal(resp)
		_, frameErr := (protocol.Frame{Payload: append(raw, make([]byte, 24)...)}).Marshal()
		if resp.OK || resp.Code != "too_large" {
			t.Errorf("zero unread metadata was not rejected: json=%d budget=%d frame=%v code=%s", len(raw), plaintextBudget(), frameErr, resp.Code)
		} else if len(raw) > plaintextBudget() || frameErr != nil {
			t.Errorf("failure itself cannot leave the PC: json=%d frame=%v", len(raw), frameErr)
		}
	}
}

func TestCompletionAttentionEmptyListDispatchRejectsOversizedMetadata(t *testing.T) {
	e := testEngine(t)
	// Each valid-size project name fits on its own; the complete sidebar can
	// fill the frame even when there is no unread completion to paginate.
	for i := 0; i < 120; i++ {
		if err := e.Store().CreateProject(&store.Project{Name: strings.Repeat("项", 200)}); err != nil {
			t.Fatal(err)
		}
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "empty-list", Op: OpList})
	pump.Dispatch(raw)
	resp := log.waitID(t, "empty-list", 2*time.Second)
	raw, _ = json.Marshal(resp)
	_, frameErr := (protocol.Frame{Payload: append(raw, make([]byte, 24)...)}).Marshal()
	if resp.OK || resp.Code != "too_large" {
		t.Fatalf("list/dispatch silently lost oversized empty inbox: json=%d budget=%d frame=%v code=%s", len(raw), plaintextBudget(), frameErr, resp.Code)
	}
	if len(raw) > plaintextBudget() || frameErr != nil {
		t.Fatalf("failure itself cannot leave the PC: json=%d frame=%v", len(raw), frameErr)
	}
}

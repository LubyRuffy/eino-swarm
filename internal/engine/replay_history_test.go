package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Observe the actual rows GORM decoded, rather than passing an already-filtered
// slice into the replay reducer. Tool overflow pages must never reach this path.
func TestReplayReadsOnlyItsHistoryRowsOnce(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "U-甲"}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", Content: "U-甲"}, {Role: "assistant", Content: "A-乙"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "tool", Content: strings.Repeat("payload", 4096)}}); err != nil {
			t.Fatal(err)
		}
		if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "tool_result", Text: strings.Repeat("payload", 4096)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"tool_call", "reasoning", "engine_restart_checkpoint"} {
		if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: kind, Text: "excluded"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "agent_message", AgentID: "worker-1", Text: "excluded worker"}); err != nil {
		t.Fatal(err)
	}
	var messageReads, eventReads int
	if err := e.store.DB().Callback().Query().After("gorm:query").Register("test:replay_rows", func(tx *gorm.DB) {
		switch rows := tx.Statement.Dest.(type) {
		case *[]store.Message:
			messageReads++
			for _, row := range *rows {
				if row.Role != "user" && row.Role != "assistant" {
					t.Errorf("replay loaded excluded message role %q (%d bytes)", row.Role, len(row.Content))
					break
				}
			}
		case *[]store.Event:
			eventReads++
			for _, row := range *rows {
				if row.Kind != "steer" && row.Kind != "steer_retracted" && (row.Kind != "agent_message" || !isManagerAgent(row.AgentID)) {
					t.Errorf("replay loaded excluded event %q", row.Kind)
					break
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	got, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history=%+v", got)
	}
	if messageReads != 1 || eventReads != 1 {
		t.Fatalf("message/event reads=%d/%d, want 1/1", messageReads, eventReads)
	}
}

// This is the former replay algorithm, frozen as a differential oracle. Its
// unrestricted Store reads intentionally remain here only, never in production.
func legacyReplayReference(t testing.TB, e *Engine, threadID, skipTurnID string) []adk.Message {
	t.Helper()
	th, err := e.store.GetThread(threadID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	through := int64(0)
	if strings.TrimSpace(th.CompactSummary) != "" {
		through = th.CompactThroughSeq
	}
	out := make([]adk.Message, 0, len(rows))
	live := map[string]bool{}
	seen := map[string]bool{}
	retracted := retractedSteerSeqs(events)
	captions := retractedSteerCaptionsFrom(events)
	for _, r := range rows {
		if (skipTurnID != "" && r.TurnID == skipTurnID) || (through > 0 && r.Seq <= through) {
			continue
		}
		live[r.TurnID] = true
		if skipRetractedSteer(r, retracted, captions) {
			continue
		}
		switch schema.RoleType(r.Role) {
		case schema.User:
			if strings.TrimSpace(r.Content) != "" || len(r.Images) > 0 {
				out = append(out, e.schemaUser(r))
				if key := strings.TrimSpace(r.Content); key != "" {
					seen[key] = true
				}
			}
		case schema.Assistant:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, schema.AssistantMessage(r.Content, nil))
				seen[strings.TrimSpace(r.Content)] = true
			}
		}
	}
	// The former appendMissingEventAnswers loaded both tables again.
	storedRows, err := e.store.ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]map[string]bool{}
	for _, r := range storedRows {
		if r.Role != "assistant" {
			continue
		}
		if stored[r.TurnID] == nil {
			stored[r.TurnID] = map[string]bool{}
		}
		stored[r.TurnID][strings.TrimSpace(r.Content)] = true
	}
	events, err = e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		text := strings.TrimSpace(ev.Text)
		if ev.Kind != "agent_message" || !isManagerAgent(ev.AgentID) || !live[ev.TurnID] || text == "" || stored[ev.TurnID][text] || seen[text] {
			continue
		}
		out = append(out, schema.AssistantMessage(ev.Text, nil))
		seen[text] = true
		if stored[ev.TurnID] == nil {
			stored[ev.TurnID] = map[string]bool{}
		}
		stored[ev.TurnID][text] = true
	}
	return out
}

func TestFilteredReplayPreservesCompactionRetractionsAndMissingAnswers(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprint("skip=", skip), func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turns := make([]*store.Turn, 5)
			for i := range turns {
				turns[i] = &store.Turn{ThreadID: th.ID}
				if err := e.store.CreateTurn(turns[i]); err != nil {
					t.Fatal(err)
				}
			}
			add := func(n int, rows ...store.Message) {
				t.Helper()
				if err := e.store.AppendMessages(th.ID, turns[n].ID, rows); err != nil {
					t.Fatal(err)
				}
			}
			event := func(n int, kind, agent, text string) store.Event {
				t.Helper()
				ev := store.Event{ThreadID: th.ID, TurnID: turns[n].ID, Kind: kind, AgentID: agent, Text: text}
				if err := e.store.AppendEvent(&ev); err != nil {
					t.Fatal(err)
				}
				return ev
			}
			add(0, store.Message{Role: "user", Content: "folded input"}, store.Message{Role: "assistant", Content: "\u3000folded answer\u00a0"})
			rows, _ := e.store.ListMessages(th.ID)
			through := rows[len(rows)-1].Seq
			if err := e.store.UpdateThread(th.ID, map[string]any{"goal": " 目标 G ", "compact_summary": " 摘要 S ", "compact_through_seq": through}); err != nil {
				t.Fatal(err)
			}
			bySeq := event(1, KindSteer, "", "caption-seq")
			byCaption := event(1, KindSteer, "", "caption-old")
			event(1, KindSteerRetracted, "", fmt.Sprintf(`{"seq":%d}`, bySeq.Seq))
			event(1, KindSteerRetracted, "", fmt.Sprintf(`{"seq":%d}`, byCaption.Seq))
			add(0, store.Message{Role: "tool", Content: "still live tool"})
			add(1, store.Message{Role: "user", Content: "[steer] caption-seq", EventSeq: bySeq.Seq}, store.Message{Role: "user", Content: "[steer] caption-old"}, store.Message{Role: "user", Content: "A甲B乙"}, store.Message{Role: "assistant", Content: "答 C"}, store.Message{Role: "user", Content: " \t "}, store.Message{Role: "assistant", Content: ""})
			add(2, store.Message{Role: "tool", Content: "tool-only"})
			add(3, store.Message{Role: "assistant", Content: " \u3000 "})
			add(4, store.Message{Role: "user", Content: "skip candidate"})
			event(0, "agent_message", swarm.DefaultManagerID, "\t folded answer\u2003")
			event(1, "agent_message", swarm.DefaultManagerID, "missing 甲")
			event(1, "agent_message", swarm.DefaultManagerID, "missing 甲")
			event(1, "agent_message", "worker-1", "worker-only")
			event(1, "agent_message", "", "A甲B乙")
			event(1, "agent_message", "", " \u3000 ")
			event(2, "agent_message", swarm.DefaultManagerID, "tool-only answer")
			event(3, "agent_message", "", "blank-row answer")
			event(4, "agent_message", swarm.DefaultManagerID, "skip answer")
			skipID := ""
			if skip {
				skipID = turns[4].ID
			}
			want := legacyReplayReference(t, e, th.ID, skipID)
			got, err := e.replayHistorySkipping(th.ID, skipID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%+v\nwant=%+v", got, want)
			}
			after, _ := e.store.ListMessages(th.ID)
			counts := map[string]int{}
			for _, m := range after {
				if m.Role == "assistant" {
					counts[strings.TrimSpace(m.Content)]++
				}
			}
			for _, text := range []string{"folded answer", "missing 甲", "tool-only answer", "blank-row answer"} {
				if counts[text] != 1 {
					t.Errorf("%q stored %d times", text, counts[text])
				}
			}
			// The first replay repairs missing answers. A second replay must not mint
			// another seq, including answers whose stored copy is below the watermark.
			if _, err := e.replayHistorySkipping(th.ID, skipID); err != nil {
				t.Fatal(err)
			}
			again, _ := e.store.ListMessages(th.ID)
			if len(again) != len(after) {
				t.Fatalf("repeat repair inserted %d rows", len(again)-len(after))
			}
			if !skip {
				current, _ := e.store.GetThread(th.ID)
				expected := len([]rune(strings.TrimSpace(current.Goal))) + len([]rune(strings.TrimSpace(current.CompactSummary)))
				for _, m := range legacyReplayReference(t, e, th.ID, "") {
					expected += len([]rune(m.Content))
				}
				chars, _, err := e.ContextUsage(th.ID)
				if err != nil || chars != expected {
					t.Fatalf("context_chars=%d want=%d err=%v", chars, expected, err)
				}
			}
		})
	}
}

func TestNextTurnUsesFilteredReplayAtTheActualModelBoundary(t *testing.T) {
	e, wire := newRestartReplayEngine(t)
	th, err := e.CreateThread("history fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", Content: "folded-user"}, {Role: "assistant", Content: "folded-answer"}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.store.ListMessages(th.ID)
	through := rows[len(rows)-1].Seq
	if err := e.store.UpdateThread(th.ID, map[string]any{"compact_summary": "summary-甲", "compact_through_seq": through}); err != nil {
		t.Fatal(err)
	}
	steer := &store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindSteer, Text: "retracted-caption"}
	if err := e.store.AppendEvent(steer); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindSteerRetracted, Text: fmt.Sprintf(`{"seq":%d}`, steer.Seq)}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "tool", Content: strings.Repeat("excluded-tool", 1024)}, {Role: "user", Content: "[steer] retracted-caption", EventSeq: steer.Seq}, {Role: "user", Content: "kept-user-甲"}, {Role: "assistant", Content: "kept-answer-乙", ToolCalls: `[{"id":"old-call"}]`}}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"folded-answer", "missing-answer-丙"} {
		if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "agent_message", AgentID: "manager", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	want := legacyReplayReference(t, e, th.ID, "")
	started, err := e.StartTurn(th.ID, "next-input-丁")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForTurn(t, e, started.ID); done.Status != store.TurnDone {
		t.Fatalf("turn did not finish: %+v", done)
	}
	request := wire.onlyRequest(t)
	var got []string
	for _, m := range request {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			t.Fatalf("past tools crossed actual model boundary: %+v", m)
		}
		if m.Role == "user" || m.Role == "assistant" {
			got = append(got, m.Role+":"+m.Content)
		}
		for _, excluded := range []string{"excluded-tool", "folded-user", "folded-answer", "retracted-caption"} {
			if strings.Contains(m.Content, excluded) {
				t.Errorf("model input resurrected %q", excluded)
			}
		}
	}
	var expected []string
	for _, m := range want {
		expected = append(expected, string(m.Role)+":"+m.Content)
	}
	expected = append(expected, "user:next-input-丁")
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("actual model history=%v want=%v", got, expected)
	}
	rows, err = e.store.ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, m := range rows {
		if m.Role == "assistant" {
			counts[strings.TrimSpace(m.Content)]++
		}
	}
	if counts["folded-answer"] != 1 || counts["missing-answer-丙"] != 1 {
		t.Fatalf("answer repair duplicates=%v", counts)
	}
}

func TestReplayReportsFailuresFromTheActualFilteredQueries(t *testing.T) {
	for _, table := range []string{"messages", "events"} {
		t.Run(table, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Exec("DROP TABLE " + table).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := e.replayHistory(th.ID); err == nil || !strings.Contains(err.Error(), "list replay "+table) {
				t.Fatalf("missing %s query failure: %v", table, err)
			}
		})
	}
}

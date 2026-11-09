package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSideChatReadSnapshotBoundsRowsAndSkipsPrivateContent(t *testing.T) {
	s := open(t)
	th := &Thread{ProviderID: "thread-provider", Model: "future-model", Goal: strings.Repeat("界", 10000)}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	other := mustThread(t, s)
	quiet := &Turn{ThreadID: th.ID, Status: TurnDone, Quiet: true}
	if err := s.CreateTurn(quiet); err != nil {
		t.Fatal(err)
	}
	active := &Turn{ThreadID: th.ID, Status: TurnRunning, Model: "active-model", UserText: strings.Repeat("界", 10000), Error: "private turn error"}
	if err := s.CreateTurn(active); err != nil {
		t.Fatal(err)
	}
	events := []Event{{ThreadID: th.ID, TurnID: active.ID, Kind: "user_message", Text: "past the bounded window"}}
	for range sideChatTailRows {
		events = append(events, Event{ThreadID: th.ID, TurnID: active.ID, Kind: "tool_result", Text: "tool result omitted"})
	}
	for i := range 15 {
		events = append(events, Event{ThreadID: th.ID, TurnID: active.ID, Kind: "agent_message", AgentID: "manager",
			Text: fmt.Sprintf("%d-", i) + strings.Repeat("界", 2000), Err: "hidden diagnostics"})
	}
	events = append(events,
		Event{ThreadID: th.ID, TurnID: quiet.ID, Kind: "agent_message", AgentID: "manager", Text: "quiet result"},
		Event{ThreadID: th.ID, TurnID: active.ID, Kind: "agent_message", AgentID: "worker", Text: "worker result"},
		Event{ThreadID: other.ID, Kind: "agent_message", AgentID: "manager", Text: "other conversation"},
	)
	for i := range events {
		if err := s.AppendEvent(&events[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ReadSideChatContext(t.Context(), th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Thread.ProviderID != th.ProviderID || got.Thread.Model != th.Model || utf8.RuneCountInString(got.Thread.Goal) != sideChatTaskCharacters {
		t.Fatalf("thread preview metadata or goal cap differs")
	}
	if got.Latest.Status != TurnRunning || got.Latest.Model != active.Model || got.Latest.Error != "" || utf8.RuneCountInString(got.Latest.UserText) != sideChatTaskCharacters {
		t.Fatalf("active turn metadata or task cap differs")
	}
	if len(got.Events) != sideChatContextRows {
		t.Fatalf("events=%d", len(got.Events))
	}
	for i, event := range got.Events {
		if !strings.HasPrefix(event.Text, fmt.Sprintf("%d-", i+3)) || utf8.RuneCountInString(event.Text) != sideChatEventCharacters || event.Err != "" {
			t.Fatalf("event %d order, cap or projection differs", i)
		}
	}

	// A thread tail dominated by tool activity does not page the whole log
	// to fill the prose quota.
	for range sideChatTailRows {
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: active.ID, Kind: "tool_result", Text: "tool output"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.ReadSideChatContext(t.Context(), th.ID)
	if err != nil || len(got.Events) != 0 || got.Latest.Status != TurnRunning {
		t.Fatalf("bounded tool-only window: events=%d latest=%q err=%v", len(got.Events), got.Latest.Status, err)
	}
}

func TestSideChatReadSnapshotPreservesMissingAndStorageErrors(t *testing.T) {
	s := open(t)
	th := mustThread(t, s)
	if _, err := s.ReadSideChatContext(t.Context(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing thread error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ReadSideChatContext(ctx, th.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled snapshot error = %v", err)
	}
	for _, table := range []string{"events", "turns", "threads"} {
		if err := s.db.Exec("DROP TABLE " + table).Error; err != nil {
			t.Fatal(err)
		}
		if got, err := s.ReadSideChatContext(t.Context(), th.ID); err == nil || !strings.Contains(err.Error(), "no such table: "+table) || got.Thread.ProviderID != "" {
			t.Fatalf("snapshot with %s missing: got=%+v err=%v", table, got, err)
		}
	}
}

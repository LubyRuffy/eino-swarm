package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWikiScanReadsOnlyCurrentObservedSourcesAndPreservesFullHistory(t *testing.T) {
	st := openTestStore(t)
	project := Project{Name: "Project"}
	if err := st.CreateProject(&project); err != nil {
		t.Fatal(err)
	}
	thread := Thread{ProjectID: project.ID}
	if err := st.CreateThread(&thread); err != nil {
		t.Fatal(err)
	}
	turn := Turn{ThreadID: thread.ID, Status: TurnDone}
	if err := st.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	event := func(kind, body, actor, call string) Event {
		t.Helper()
		e := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: kind, Text: body, AgentID: actor, ToolCallID: call}
		if err := st.AppendEvent(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	human := event("user_message", "Retained message.", "", "")
	call := event("tool_call", "observe("+strings.Repeat("argument ", 550)+")", "worker", "shared")
	event("tool_call", "unrelated()", "another-worker", "shared")
	success := event("tool_result", `{"full_command":"private argument","exit_code":0,"stdout":"observable text","stderr":"warning"}`, "worker", "shared")
	failed := event("tool_result", `{"ok":false,"data":"not evidence"}`, "worker", "shared")
	orphan := event("tool_result", "No matching invocation.", "another-worker", "missing")
	steer := event("steer", "Stale caption.", "", "")
	if err := st.AppendMessages(thread.ID, turn.ID, []Message{{Role: "user", Content: "[steer] Current caption.", EventSeq: steer.Seq}}); err != nil {
		t.Fatal(err)
	}
	// The fixed prefix should stop before the multibyte rune that crosses it.
	full := strings.Repeat("a", wikiScanBytes-1) + "測" + strings.Repeat("b", 100) + " tail"
	large := event("agent_message", full, "worker", "")
	vanished := event("user_message", "Source removed after the snapshot.", "", "")
	job := ProjectDistillation{ProjectID: project.ID, ID: "wiki-local", State: "running", WikiGraph: true}
	receipt := func(int) ([]byte, error) { return []byte(`{}`), nil }
	wantIDs := []uint{human.ID, success.ID, failed.ID, orphan.ID, steer.ID, large.ID, vanished.ID}
	if n, err := st.StartDistillationQueue(t.Context(), job, thread.ID, receipt); err != nil || n != len(wantIDs) {
		t.Fatalf("frozen source scope differs: %d %v", n, err)
	}
	if err := st.DB().Delete(&Event{}, vanished.ID).Error; err != nil {
		t.Fatal(err)
	}
	event("user_message", "Arrived too late.", "", "")
	head, err := st.WikiScanWindow(t.Context(), job.ID, 0, 2)
	if err != nil || len(head) != 2 {
		t.Fatalf("first local window: %+v %v", head, err)
	}
	if got := head[0]; got.Position != human.ID || !got.Available || got.Part.Text != human.Text || got.Part.TotalBytes != len(human.Text) {
		t.Fatalf("human evidence was not read exactly: %+v", got)
	}
	if got := head[1]; got.Position != success.ID || !got.Available || got.Part.Text != "observable text\nwarning" || got.CallID != call.ID || !strings.HasPrefix(got.CallHead, "observe(") {
		t.Fatalf("tool evidence used an argument, wrong call, or stale body: %+v", got)
	}
	tail, err := st.WikiScanWindow(t.Context(), job.ID, success.ID, 128)
	if err != nil || len(tail) != len(wantIDs)-2 {
		t.Fatalf("tail did not preserve the original frozen scope: %+v %v", tail, err)
	}
	for i, row := range tail {
		if row.Position != wantIDs[i+2] {
			t.Fatalf("frozen cursor skipped or reordered a position: %d want %d", row.Position, wantIDs[i+2])
		}
	}
	for _, i := range []int{0, 1, 4} {
		if tail[i].Available || tail[i].Part.Text != "" {
			t.Fatalf("failed, unpaired, or removed evidence was published: %d %+v", i, tail[i])
		}
	}
	if got := tail[2]; !got.Available || got.Part.Text != "Current caption." || got.Part.MessageSeq <= 0 {
		t.Fatalf("editable steering evidence did not use its current caption: %+v", got)
	}
	prefix := tail[3].Part
	if !tail[3].Available || prefix.TotalBytes != len(full) || len(prefix.Text) != wikiScanBytes-1 || !utf8.ValidString(prefix.Text) {
		t.Fatalf("the bounded prefix was truncated mid-rune or lost its length: prefix=%d total=%d", len(prefix.Text), prefix.TotalBytes)
	}
	part, err := st.ReadWikiSourceText(t.Context(), job.ID, large.ID, len(prefix.Text), 512)
	if err != nil || prefix.Text+part.Text != full {
		t.Fatalf("the existing full-text fallback lost the original suffix: %+v %v", part, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := st.WikiScanWindow(ctx, job.ID, 0, 128); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled local scan did not preserve its cause: %v", err)
	}
}

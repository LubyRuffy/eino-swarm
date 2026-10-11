package store

import (
	"testing"
	"time"
)

// The open conversation has to name every wake still aimed at it, and the
// job whose fire minted it, without dragging in a neighbour or a cancelled
// wake that only used to land here.
func TestConversationListsTheWakeAimedHereAndTheJobThatMintedIt(t *testing.T) {
	s := openTestStore(t)
	home := mustThread(t, s)
	other := mustThread(t, s)
	minted := mustThread(t, s)
	legacyChat := mustThread(t, s)
	oldMint := mustThread(t, s)

	soon := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	later := soon.Add(2 * time.Hour)
	wake := mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: home.ID, OriginThreadID: home.ID,
		Title: "aimed here", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: soon, Status: ScheduleActive,
	})
	paused := mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: home.ID, OriginThreadID: home.ID,
		Title: "paused here", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: later, Status: SchedulePaused,
	})
	finished := mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: home.ID, OriginThreadID: home.ID,
		Title: "finished here", Prompt: "Continue the wait.", DelayS: 60,
		NextRunAt: soon.Add(time.Hour), Status: ScheduleDone,
	})
	mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: home.ID, OriginThreadID: home.ID,
		Title: "cancelled here", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: soon, Status: ScheduleCancelled,
	})
	elsewhere := mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: other.ID, OriginThreadID: other.ID,
		Title: "aimed elsewhere", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: soon, Status: ScheduleActive,
	})
	// A wake's own fire is not "this job created the chat".
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: wake.ID, ThreadID: home.ID, Kind: ScheduleThread, Status: ScheduleRunQuiet,
	}); err != nil {
		t.Fatal(err)
	}
	mint := mustSchedule(t, s, &Schedule{
		Kind: ScheduleStandalone, OriginThreadID: home.ID,
		Title: "minted it", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: soon, Status: ScheduleActive,
	})
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: mint.ID, ThreadID: minted.ID, Kind: ScheduleStandalone, Status: ScheduleRunFindings,
	}); err != nil {
		t.Fatal(err)
	}
	legacy := mustSchedule(t, s, &Schedule{
		Kind: ScheduleStandalone, Title: "legacy mint", Prompt: "Continue the wait.",
		EveryS: 3600, NextRunAt: later, Status: ScheduleActive,
	})
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: legacy.ID, ThreadID: legacyChat.ID, Kind: "", Status: ScheduleRunQuiet,
	}); err != nil {
		t.Fatal(err)
	}
	// Claim-time kind still points at the chat after the row moves elsewhere.
	moved := mustSchedule(t, s, &Schedule{
		Kind: ScheduleThread, ThreadID: other.ID, OriginThreadID: home.ID,
		Title: "moved after mint", Prompt: "Continue the wait.", EveryS: 3600,
		NextRunAt: soon, Status: ScheduleActive,
	})
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: moved.ID, ThreadID: oldMint.ID, Kind: ScheduleStandalone, Status: ScheduleRunQuiet,
	}); err != nil {
		t.Fatal(err)
	}
	// An empty kind on a wake fire must not count as a mint once the row is a thread wake.
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: wake.ID, ThreadID: other.ID, Kind: "", Status: ScheduleRunQuiet,
	}); err != nil {
		t.Fatal(err)
	}
	cancelledMint := mustSchedule(t, s, &Schedule{
		Kind: ScheduleStandalone, Title: "cancelled mint", Prompt: "Continue the wait.",
		EveryS: 3600, NextRunAt: later, Status: ScheduleCancelled,
	})
	if err := s.CreateRun(&ScheduleRun{
		ScheduleID: cancelledMint.ID, ThreadID: minted.ID, Kind: ScheduleStandalone,
		Status: ScheduleRunQuiet,
	}); err != nil {
		t.Fatal(err)
	}

	blank, err := s.SchedulesForThread("  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(blank.Schedules) != 0 || len(blank.CreatedBy) != 0 {
		t.Fatalf("blank thread: %+v", blank)
	}

	homeRows, err := s.SchedulesForThread(home.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scheduleRowIDs(homeRows.Schedules), []string{wake.ID, paused.ID, finished.ID}; !sameIDs(got, want) {
		t.Fatalf("home schedules=%v want %v", got, want)
	}
	if homeRows.Schedules[0].ID != wake.ID || homeRows.Schedules[1].ID != paused.ID || homeRows.Schedules[2].ID != finished.ID {
		t.Fatalf("home order=%v", scheduleRowIDs(homeRows.Schedules))
	}
	if len(homeRows.CreatedBy) != 0 {
		t.Fatalf("a wake fire is not a mint: %v", homeRows.CreatedBy)
	}

	mintRows, err := s.SchedulesForThread(minted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scheduleRowIDs(mintRows.Schedules), []string{mint.ID, cancelledMint.ID}; !sameIDs(got, want) {
		t.Fatalf("minted schedules=%v want %v", got, want)
	}
	if mintRows.Schedules[0].ID != mint.ID {
		t.Fatalf("live mint should lead: %v", scheduleRowIDs(mintRows.Schedules))
	}
	if got, want := mintRows.CreatedBy, []string{cancelledMint.ID, mint.ID}; !sameIDs(got, want) {
		t.Fatalf("created_by=%v want %v", got, want)
	}

	legacyRows, err := s.SchedulesForThread(legacyChat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRows.Schedules) != 1 || legacyRows.Schedules[0].ID != legacy.ID {
		t.Fatalf("legacy mint=%v", scheduleRowIDs(legacyRows.Schedules))
	}

	oldRows, err := s.SchedulesForThread(oldMint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldRows.Schedules) != 1 || oldRows.Schedules[0].ID != moved.ID || len(oldRows.CreatedBy) != 1 {
		t.Fatalf("moved mint=%+v", oldRows)
	}

	otherRows, err := s.SchedulesForThread(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scheduleRowIDs(otherRows.Schedules), []string{elsewhere.ID, moved.ID}; !sameIDs(got, want) {
		t.Fatalf("other schedules=%v want %v", got, want)
	}
	if len(otherRows.CreatedBy) != 0 || containsID(scheduleRowIDs(otherRows.Schedules), wake.ID) {
		t.Fatalf("empty-kind wake fire counted as a mint: %+v", otherRows)
	}
}

func TestSchedulesForThreadReportsAClosedStore(t *testing.T) {
	s := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SchedulesForThread("th_closed"); err == nil {
		t.Fatal("closed store returned a list")
	}
}

func mustThread(t *testing.T, s *Store) *Thread {
	t.Helper()
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	return th
}

func mustSchedule(t *testing.T, s *Store, row *Schedule) *Schedule {
	t.Helper()
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	return row
}

func scheduleRowIDs(rows []Schedule) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

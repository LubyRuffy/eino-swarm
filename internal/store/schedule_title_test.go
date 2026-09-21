package store

import "testing"

// ApplyAutoScheduleTitle is a compare-and-swap: a PATCH in between must
// win, otherwise a slow namer puts the generated name back over what they typed.
func TestApplyAutoScheduleTitleOnlyWhileMachineOwned(t *testing.T) {
	s := openTestStore(t)
	row := &Schedule{
		Kind: ScheduleStandalone, Title: "placeholder", TitleAuto: true,
		Prompt: "Continue the wait.", EveryS: 60, CreatedBy: ScheduleCreatedHuman,
	}
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	ok, err := s.ApplyAutoScheduleTitle(row.ID, " Generated name ")
	if err != nil || !ok {
		t.Fatalf("ApplyAutoScheduleTitle: ok=%v err=%v", ok, err)
	}
	got, _ := s.GetSchedule(row.ID)
	if got.Title != "Generated name" || got.TitleAuto {
		t.Fatalf("landed title=%+v", got)
	}
	ok, err = s.ApplyAutoScheduleTitle(row.ID, "second try")
	if err != nil || ok {
		t.Fatalf("a second apply must not overwrite: ok=%v err=%v", ok, err)
	}
	got, _ = s.GetSchedule(row.ID)
	if got.Title != "Generated name" {
		t.Fatalf("second apply overwrote: %q", got.Title)
	}

	owned := &Schedule{
		Kind: ScheduleStandalone, Title: "placeholder", TitleAuto: true,
		Prompt: "Continue the wait.", EveryS: 60, CreatedBy: ScheduleCreatedHuman,
	}
	if err := s.CreateSchedule(owned); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(owned.ID, map[string]any{"title": "Mine", "title_auto": false}); err != nil {
		t.Fatal(err)
	}
	ok, err = s.ApplyAutoScheduleTitle(owned.ID, "generated")
	if err != nil || ok {
		t.Fatalf("rename must beat the namer: ok=%v err=%v", ok, err)
	}
	got, _ = s.GetSchedule(owned.ID)
	if got.Title != "Mine" {
		t.Fatalf("namer overwrote a rename: %q", got.Title)
	}
	ok, err = s.ApplyAutoScheduleTitle("missing", "x")
	if err != nil || ok {
		t.Fatalf("missing id: ok=%v err=%v", ok, err)
	}
	ok, err = s.ApplyAutoScheduleTitle(row.ID, "  ")
	if err != nil || ok {
		t.Fatalf("blank title: ok=%v err=%v", ok, err)
	}
}

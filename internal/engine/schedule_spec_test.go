package engine

import (
	"testing"
	"time"
)

func TestNextRunAfterIntervalDoesNotCatchUp(t *testing.T) {
	// Missed beats are not skipped by looping until a future slot; the
	// engine fires once then asks for the next instant from now.
	// Seconds on `now` matter: aligning to 12:01:00 would look like
	// now+60s when now is 12:00:00, which hides a catch-up bug.
	spec := scheduleSpec{every: 60 * time.Second}
	now := time.Date(2026, 9, 18, 12, 0, 17, 0, time.UTC)
	next := spec.nextAfter(now)
	if !next.Equal(now.Add(60 * time.Second)) {
		t.Fatalf("next=%s want=%s", next, now.Add(60*time.Second))
	}
	aligned := time.Date(2026, 9, 18, 12, 1, 0, 0, time.UTC)
	if next.Equal(aligned) {
		t.Fatal("aligned to the minute; that is catch-up")
	}
}

func TestNextRunCronWeekdayMorning(t *testing.T) {
	spec, err := parseScheduleSpec(0, 0, "0 9 * * 1-5", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("test", 2*3600)
	spec.loc = loc
	// Friday 10:00 in the test zone: the 09:00 weekday slot is already
	// past, so the next fire is Monday morning — not a weekend hour.
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, loc)
	next := spec.nextAfter(now)
	if !next.After(now) {
		t.Fatalf("next=%s not after now=%s", next, now)
	}
	if next.Location() != time.UTC {
		t.Fatalf("cron next must be UTC, loc=%s", next.Location())
	}
	want := time.Date(2026, 9, 21, 9, 0, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
	later := spec.nextAfter(next)
	if !later.After(next) {
		t.Fatalf("following slot %s is not after %s", later, next)
	}
	wantLater := time.Date(2026, 9, 22, 9, 0, 0, 0, loc).UTC()
	if !later.Equal(wantLater) {
		t.Fatalf("later=%s want=%s", later, wantLater)
	}
}

func TestNextRunCronSkipsTheInstantThatIsNow(t *testing.T) {
	spec, err := parseScheduleSpec(0, 0, "0 9 * * 1-5", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("test", 0)
	spec.loc = loc
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, loc)
	next := spec.nextAfter(now)
	want := time.Date(2026, 9, 22, 9, 0, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("strictly after now: next=%s want=%s", next, want)
	}
}

func TestParseSpecRejectsBelowMinInterval(t *testing.T) {
	_, err := parseScheduleSpec(0, 1, "", 30*time.Second)
	if err == nil {
		t.Fatal("1s every must fail a 30s floor")
	}
}

func TestParseSpecRejectsDelayBelowMin(t *testing.T) {
	_, err := parseScheduleSpec(1, 0, "", 30*time.Second)
	if err == nil {
		t.Fatal("1s delay must fail a 30s floor")
	}
}

func TestParseSpecRequiresExactlyOneCadence(t *testing.T) {
	_, err := parseScheduleSpec(10, 10, "", time.Second)
	if err == nil {
		t.Fatal("delay and every together")
	}
}

func TestParseSpecRejectsEmptyCadence(t *testing.T) {
	_, err := parseScheduleSpec(0, 0, "", 30*time.Second)
	if err == nil {
		t.Fatal("empty spec")
	}
	_, err = parseScheduleSpec(0, 0, "   ", 30*time.Second)
	if err == nil {
		t.Fatal("blank cron")
	}
}

func TestParseSpecRejectsCronCombinedWithEvery(t *testing.T) {
	_, err := parseScheduleSpec(0, 60, "0 9 * * 1-5", 30*time.Second)
	if err == nil {
		t.Fatal("every and cron together")
	}
}

func TestParseSpecRejectsCronCombinedWithDelay(t *testing.T) {
	_, err := parseScheduleSpec(60, 0, "0 9 * * *", 30*time.Second)
	if err == nil {
		t.Fatal("delay and cron together")
	}
}

func TestParseDelayNextAfterIsZero(t *testing.T) {
	// One-shot: the engine stamps the first due time at create. Asking
	// this helper again must not queue a second delay fire.
	spec, err := parseScheduleSpec(60, 0, "", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if got := spec.nextAfter(now); !got.IsZero() {
		t.Fatalf("delay nextAfter=%s, want zero", got)
	}
}

func TestParseCronIsNotFlooredByMinInterval(t *testing.T) {
	if _, err := parseScheduleSpec(0, 0, "0 9 * * *", 30*time.Second); err != nil {
		t.Fatalf("daily wall-clock is not an interval: %v", err)
	}
}

func TestParseCronFieldPatterns(t *testing.T) {
	loc := time.FixedZone("test", 0)
	spec, err := parseScheduleSpec(0, 0, "*/15 8-9,17 1-3/2 * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	spec.loc = loc
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, loc)
	next := spec.nextAfter(now)
	want := time.Date(2026, 9, 1, 8, 15, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}

	// n/s is start-at-n, not a range: 5, 35.
	step, err := parseScheduleSpec(0, 0, "5/30 0 * * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	step.loc = loc
	got := step.nextAfter(time.Date(2026, 9, 1, 0, 0, 0, 0, loc))
	wantStep := time.Date(2026, 9, 1, 0, 5, 0, 0, loc).UTC()
	if !got.Equal(wantStep) {
		t.Fatalf("start-at-n step next=%s want=%s", got, wantStep)
	}
}

func TestParseCronSundaySeven(t *testing.T) {
	spec, err := parseScheduleSpec(0, 0, "0 0 * * 7", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("test", 0)
	spec.loc = loc
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, loc) // Friday
	next := spec.nextAfter(now)
	want := time.Date(2026, 9, 20, 0, 0, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("dow 7 is Sunday: next=%s want=%s", next, want)
	}
}

func TestParseCronDomOrDow(t *testing.T) {
	// Both fields restricted: Vixie ORs them, so a Monday that is not
	// the 1st still matches.
	spec, err := parseScheduleSpec(0, 0, "0 0 1 * 1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("test", 0)
	spec.loc = loc
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, loc) // Sunday
	next := spec.nextAfter(now)
	want := time.Date(2026, 9, 21, 0, 0, 0, 0, loc).UTC() // Monday
	if !next.Equal(want) {
		t.Fatalf("dow OR dom: next=%s want=%s", next, want)
	}
}

func TestParseCronRejectsGarbage(t *testing.T) {
	min := 30 * time.Second
	cases := []string{
		"* * * *",
		"0 9 * * 1-5 extra",
		"60 * * * *",
		"0 24 * * *",
		"0 9 0 * *",
		"0 9 * 0 *",
		"0 9 * * 8",
		"0 9 * * 5-1",
		"0 9 * * */0",
		"0 9 * * 1/",
		"0 9 * * a",
		"0 9 * * ,1",
		"0 9 * * 1-",
		"0 9 * * x-5",
		"0 9 * * 1-x",
		"0-99 * * * *",
	}
	for _, cron := range cases {
		if _, err := parseScheduleSpec(0, 0, cron, min); err == nil {
			t.Errorf("accepted %q", cron)
		}
	}
}

func TestParseIntervalRoundTrip(t *testing.T) {
	spec, err := parseScheduleSpec(0, 60, "", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if got := spec.nextAfter(now); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("parsed every next=%s", got)
	}
}

func TestNextRunCronUnsatisfiableIsZero(t *testing.T) {
	spec, err := parseScheduleSpec(0, 0, "0 0 31 2 *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("test", 0)
	spec.loc = loc
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, loc)
	if got := spec.nextAfter(now); !got.IsZero() {
		t.Fatalf("no matching wall-clock: next=%s", got)
	}
}

func TestNextRunCronNilLocationUsesHostZone(t *testing.T) {
	spec, err := parseScheduleSpec(0, 0, "* * * * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	spec.loc = nil
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	next := spec.nextAfter(now)
	if next.IsZero() || !next.After(now) || next.Location() != time.UTC {
		t.Fatalf("next=%s loc=%v", next, next.Location())
	}
}

func loadTestLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func nextAfterBounded(t *testing.T, spec scheduleSpec, now time.Time) time.Time {
	t.Helper()
	ch := make(chan time.Time, 1)
	go func() { ch <- spec.nextAfter(now) }()
	select {
	case next := <-ch:
		return next
	case <-time.After(2 * time.Second):
		t.Fatal("nextAfter stalled")
		return time.Time{}
	}
}

func TestNextRunCronSpringForwardDSTDoesNotHang(t *testing.T) {
	loc := loadTestLocation(t, "America/New_York")
	spec, err := parseScheduleSpec(0, 0, "0 9 * * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	spec.loc = loc
	// Saturday 09:00 before the missing hour: next wall-clock 09:00 is Sunday.
	now := time.Date(2026, 3, 7, 9, 0, 0, 0, loc)
	next := nextAfterBounded(t, spec, now)
	if !next.After(now) {
		t.Fatalf("next=%s not after now=%s", next, now)
	}
	want := time.Date(2026, 3, 8, 9, 0, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestNextRunCronSpringForwardDSTGapHour(t *testing.T) {
	loc := loadTestLocation(t, "America/New_York")
	spec, err := parseScheduleSpec(0, 0, "0 2 * * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	spec.loc = loc
	now := time.Date(2026, 3, 7, 2, 0, 0, 0, loc)
	next := nextAfterBounded(t, spec, now)
	if !next.After(now) {
		t.Fatalf("next=%s not after now=%s", next, now)
	}
}

func TestNextRunCronFallBackDSTIsStrictlyAfter(t *testing.T) {
	loc := loadTestLocation(t, "America/New_York")
	spec, err := parseScheduleSpec(0, 0, "0 1 * * *", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	spec.loc = loc
	// Second 01:00 (standard time) after the repeated hour.
	first := time.Date(2026, 11, 1, 1, 0, 0, 0, loc)
	now := first.Add(time.Hour)
	next := nextAfterBounded(t, spec, now)
	if !next.After(now) {
		t.Fatalf("next=%s unix=%d now unix=%d", next, next.Unix(), now.Unix())
	}
}

func TestParseCronRejectsOverflowStep(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := parseScheduleSpec(0, 0, "59/9223372036854775807 * * * *", time.Second)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("overflow step must be rejected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("overflow step stalled")
	}
}

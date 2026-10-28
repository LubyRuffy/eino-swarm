package engine

import (
	"testing"
	"time"
)

// cronZones include both daylight-saving directions; a scheduled check that
// skips or repeats a fire around a transition is a missed or doubled run.
var cronZones = []string{"UTC", "America/New_York", "Europe/London", "Australia/Lord_Howe", "Asia/Shanghai"}

func (c cronExpr) matchesWall(t time.Time) bool {
	return c.minute&(1<<uint(t.Minute())) != 0 &&
		c.hour&(1<<uint(t.Hour())) != 0 &&
		c.month&(1<<uint(t.Month())) != 0 &&
		c.dayMatches(t)
}

// bruteNextAfter walks every absolute minute after now and returns the first
// whose wall clock in loc matches, or zero when none does within limit.
func bruteNextAfter(c cronExpr, now time.Time, loc *time.Location, limit time.Duration) time.Time {
	t := now.Truncate(time.Minute).Add(time.Minute)
	for end := now.Add(limit); !t.After(end); t = t.Add(time.Minute) {
		if c.matchesWall(t.In(loc)) {
			return t
		}
	}
	return time.Time{}
}

// A daily check at the first hour after a spring-forward gap used to skip
// that whole day: time.Date mapped the missing 02:00 back to 01:00 and the
// fallback stepped a full hour, past 03:00.
func TestDailyCronFiresOnTheDaylightSavingStartDay(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("time zone data unavailable: %v", err)
	}
	c, err := parseCron("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 1, 0, 0, 0, ny)
	want := time.Date(2026, 3, 8, 3, 0, 0, 0, ny)
	if got := c.nextAfter(now, ny); !got.Equal(want) {
		t.Fatalf("daily 03:00 after %s fired at %s, want %s", now, got.In(ny), want)
	}
}

const cronFuzzWindow = 8 * 24 * time.Hour

// The scheduler computes each cron fire with nextAfter. It must return the
// first matching wall-clock minute strictly after now: an earlier answer
// fires early, a later one silently skips a run the user scheduled.
func FuzzCronNextAfterIsTheFirstMatchingMinute(f *testing.F) {
	start := time.Date(2026, 3, 7, 22, 13, 41, 0, time.UTC).Unix()
	for _, seed := range []struct {
		expr string
		at   int64
		zone uint8
	}{
		{"*/15 * * * *", start, 0},
		{"30 2 * * *", time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC).Unix(), 1},  // New York spring-forward gap
		{"30 1 * * *", time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC).Unix(), 1}, // New York repeated hour
		{"0 0 29 2 *", start, 2},
		{"0 9 * * 1-5", start, 3},
		{"5 4 1,15 * 7", start, 1},
		{"0 */2 * 3 0", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix(), 2},
		{"59 23 31 12 *", time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC).Unix(), 0},
		{"0 2 * * *", time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC).Unix(), 3}, // Lord Howe half-hour shift
	} {
		f.Add(seed.expr, seed.at, seed.zone)
	}
	zones := make([]*time.Location, len(cronZones))
	for i, name := range cronZones {
		loc, err := time.LoadLocation(name)
		if err != nil {
			f.Skipf("time zone data unavailable: %v", err)
		}
		zones[i] = loc
	}
	lo := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	hi := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	f.Fuzz(func(t *testing.T, expr string, at int64, zone uint8) {
		c, err := parseCron(expr)
		if err != nil {
			return
		}
		if at < lo || at > hi {
			at = lo + (at%(hi-lo)+(hi-lo))%(hi-lo)
		}
		now := time.Unix(at, 0).UTC()
		loc := zones[int(zone)%len(zones)]
		got := c.nextAfter(now, loc)
		want := bruteNextAfter(c, now, loc, cronFuzzWindow)
		if !got.IsZero() {
			if !got.After(now) {
				t.Fatalf("%q in %s after %s: nextAfter %s is not after now", expr, loc, now, got)
			}
			if !c.matchesWall(got.In(loc)) {
				t.Fatalf("%q in %s after %s: nextAfter %s does not match", expr, loc, now, got.In(loc))
			}
		}
		if want.IsZero() {
			if !got.IsZero() && !got.After(now.Add(cronFuzzWindow)) {
				t.Fatalf("%q in %s after %s: nextAfter %s, but no minute matches before %s",
					expr, loc, now, got.In(loc), now.Add(cronFuzzWindow).In(loc))
			}
			return
		}
		if !got.Equal(want) {
			t.Fatalf("%q in %s after %s: nextAfter %s, first matching minute is %s",
				expr, loc, now.In(loc), got.In(loc), want.In(loc))
		}
	})
}

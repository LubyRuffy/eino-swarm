package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// scheduleSpec is one cadence. Exactly one of delay, every, or cron is set.
type scheduleSpec struct {
	delay time.Duration
	every time.Duration
	cron  cronExpr
	// loc is the wall-clock zone for cron (host local at parse). Tests
	// override it; production never names a zone.
	loc *time.Location
}

type cronExpr struct {
	minute, hour, dom, month, dow uint64
	domStar, dowStar              bool
}

// parseScheduleSpec accepts exactly one of delay_s, every_s, or a 5-field
// cron. delay and every below min are errors; cron is a wall-clock pattern
// and is not floored as an interval. Cron fields are interpreted in the
// host timezone; nextAfter returns UTC.
func parseScheduleSpec(delayS, everyS int, cron string, min time.Duration) (scheduleSpec, error) {
	cron = strings.TrimSpace(cron)
	n := 0
	if delayS != 0 {
		n++
	}
	if everyS != 0 {
		n++
	}
	if cron != "" {
		n++
	}
	if n != 1 {
		return scheduleSpec{}, fmt.Errorf("engine: schedule needs exactly one cadence")
	}
	switch {
	case delayS != 0:
		d := time.Duration(delayS) * time.Second
		if d < min {
			return scheduleSpec{}, fmt.Errorf("engine: delay is shorter than the minimum interval")
		}
		return scheduleSpec{delay: d}, nil
	case everyS != 0:
		d := time.Duration(everyS) * time.Second
		if d < min {
			return scheduleSpec{}, fmt.Errorf("engine: interval is shorter than the minimum interval")
		}
		return scheduleSpec{every: d}, nil
	default:
		expr, err := parseCron(cron)
		if err != nil {
			return scheduleSpec{}, err
		}
		return scheduleSpec{cron: expr, loc: time.Local}, nil
	}
}

// nextAfter reports the following fire after now.
//
// Interval: now+every, with no catch-up loop. The engine fires once then
// asks again from now.
// Cron: the next matching wall-clock in loc, returned UTC, strictly after now.
// Delay: always zero. The first due time is now+delay at create; this helper
// must not queue a second one-shot fire. The caller marks the row done.
func (s scheduleSpec) nextAfter(now time.Time) time.Time {
	if s.delay != 0 {
		return time.Time{}
	}
	if s.every > 0 {
		return now.Add(s.every)
	}
	loc := s.loc
	if loc == nil {
		loc = time.Local
	}
	next := s.cron.nextAfter(now, loc)
	if next.IsZero() {
		return next
	}
	return next.UTC()
}

func parseCron(s string) (cronExpr, error) {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return cronExpr{}, fmt.Errorf("engine: cron needs five fields")
	}
	var expr cronExpr
	var err error
	if expr.minute, _, err = parseCronField(fields[0], 0, 59, false); err != nil {
		return cronExpr{}, err
	}
	if expr.hour, _, err = parseCronField(fields[1], 0, 23, false); err != nil {
		return cronExpr{}, err
	}
	if expr.dom, expr.domStar, err = parseCronField(fields[2], 1, 31, false); err != nil {
		return cronExpr{}, err
	}
	if expr.month, _, err = parseCronField(fields[3], 1, 12, false); err != nil {
		return cronExpr{}, err
	}
	if expr.dow, expr.dowStar, err = parseCronField(fields[4], 0, 7, true); err != nil {
		return cronExpr{}, err
	}
	return expr, nil
}

func parseCronField(tok string, lo, hi int, dow bool) (bits uint64, star bool, err error) {
	if tok == "*" {
		return cronBits(lo, hi, 1), true, nil
	}
	for _, part := range strings.Split(tok, ",") {
		b, err := parseCronAtom(part, lo, hi)
		if err != nil {
			return 0, false, err
		}
		bits |= b
	}
	if dow && bits&(1<<7) != 0 {
		bits |= 1 << 0
	}
	return bits, false, nil
}

func parseCronAtom(part string, lo, hi int) (uint64, error) {
	if part == "" {
		return 0, fmt.Errorf("engine: empty cron list item")
	}
	step := 1
	base := part
	hasStep := false
	if i := strings.IndexByte(part, '/'); i >= 0 {
		base = part[:i]
		rest := part[i+1:]
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("engine: cron step")
		}
		step = n
		hasStep = true
	}
	var a, b int
	switch {
	case base == "*":
		a, b = lo, hi
	case strings.Contains(base, "-"):
		j := strings.IndexByte(base, '-')
		if j == 0 || j == len(base)-1 {
			return 0, fmt.Errorf("engine: cron range")
		}
		start, err := strconv.Atoi(base[:j])
		if err != nil {
			return 0, fmt.Errorf("engine: cron range")
		}
		end, err := strconv.Atoi(base[j+1:])
		if err != nil {
			return 0, fmt.Errorf("engine: cron range")
		}
		a, b = start, end
	default:
		n, err := strconv.Atoi(base)
		if err != nil {
			return 0, fmt.Errorf("engine: cron value")
		}
		a = n
		if hasStep {
			b = hi
		} else {
			b = n
		}
	}
	if a < lo || b > hi || a > b {
		return 0, fmt.Errorf("engine: cron field out of range")
	}
	return cronBits(a, b, step), nil
}

func cronBits(a, b, step int) uint64 {
	var bits uint64
	for v := a; v <= b; v += step {
		bits |= 1 << uint(v)
	}
	return bits
}

func (c cronExpr) nextAfter(now time.Time, loc *time.Location) time.Time {
	t := now.In(loc)
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc).Add(time.Minute)
	end := t.AddDate(5, 0, 0)
	for t.Before(end) {
		if c.month&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if c.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if c.minute&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

func (c cronExpr) dayMatches(t time.Time) bool {
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0
	if c.domStar && c.dowStar {
		return true
	}
	if c.domStar {
		return dow
	}
	if c.dowStar {
		return dom
	}
	return dom || dow
}

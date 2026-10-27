package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A refusal that only said "shorter than the minimum interval" left the
// model guessing a number; it retried several shorter waits before landing
// on one the engine accepted. The refusal must state the floor in force.
func TestScheduleWakeRefusalStatesTheMinimumInterval(t *testing.T) {
	e := newTestEngine(t)
	floor := int(e.cfg.Swarm.ScheduleMinInterval().Seconds())
	short := floor - 1
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	tl := mustInvokable(t, ScheduleWakeTool(func(args string) (string, error) {
		return e.scheduleWakeJSON(th.ID, turn.ID, args)
	}))

	for _, field := range []string{"delay_s", "every_s"} {
		out, err := tl.InvokableRun(context.Background(),
			fmt.Sprintf(`{"prompt":%q,"%s":%d,"title":"wake"}`, scheduleToolWaitPrompt, field, short))
		text := out
		if err != nil {
			text += err.Error()
		}
		want := fmt.Sprintf("%ds is shorter than the minimum interval of %ds", short, floor)
		if !strings.Contains(text, want) {
			t.Fatalf("%s=%d: refusal %q does not state %q", field, short, text, want)
		}
	}
	if rows, err := e.ListSchedules(); err != nil || len(rows) != 0 {
		t.Fatalf("a refused wait was armed: rows=%v err=%v", rows, err)
	}
}

func TestMinSecondsNamesAnAcceptedFloor(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want int64
	}{{30_000, 30}, {30_001, 31}, {500, 1}, {0, 0}} {
		if got := minSeconds(time.Duration(tc.ms) * time.Millisecond); got != tc.want {
			t.Fatalf("minSeconds(%dms)=%d want %d", tc.ms, got, tc.want)
		}
	}
}

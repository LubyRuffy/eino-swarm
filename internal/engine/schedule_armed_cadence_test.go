package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The answer check refuses a recurring claim that only a one-shot wake backs.
// A model that arms `delay_s` and then describes it as a repeating check has
// no way to learn that from the bare `{"ok":true,"id":…}` receipt, so it was
// refused once, re-armed the identical wake to justify itself, and was
// refused again. The receipt says which cadence was armed.
func TestArmedWaitReceiptStatesWhetherItRepeats(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
		deny []string
	}{
		{"one-shot", `{"delay_s":60,"prompt":"inspect later"}`, []string{"one-shot", "once", "60s"}, []string{"recurring", "fires every"}},
		{"interval", `{"every_s":90,"prompt":"inspect later"}`, []string{"recurring", "every 90s"}, []string{"one-shot"}},
		{"cron", `{"cron":"*/5 * * * *","prompt":"inspect later"}`, []string{"recurring", "*/5 * * * *"}, []string{"one-shot"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			out, err := e.scheduleWakeJSON(th.ID, turn.ID, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			var receipt struct {
				OK      bool   `json:"ok"`
				ID      string `json:"id"`
				Cadence string `json:"cadence"`
			}
			if err := json.Unmarshal([]byte(out), &receipt); err != nil || !receipt.OK || receipt.ID == "" {
				t.Fatalf("receipt lost ok/id, which the answer check reads: %s %v", out, err)
			}
			for _, w := range tc.want {
				if !strings.Contains(receipt.Cadence, w) {
					t.Errorf("cadence %q does not say %q", receipt.Cadence, w)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(receipt.Cadence, d) {
					t.Errorf("cadence %q wrongly says %q", receipt.Cadence, d)
				}
			}
		})
	}
}

func TestReplacedWaitReceiptDescribesTheNewCadence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if _, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"every_s":90,"prompt":"inspect later"}`); err != nil {
		t.Fatal(err)
	}
	// An open wake is replaced, not duplicated; the receipt must describe what
	// the replacement now is, not the row it overwrote.
	out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"delay_s":45,"prompt":"inspect later"}`)
	if err != nil || !strings.Contains(out, "one-shot") || strings.Contains(out, "every 90s") {
		t.Fatalf("replacement receipt: %s %v", out, err)
	}
}

func TestScheduleCadenceNoteIsEmptyWithoutACadence(t *testing.T) {
	if got := scheduleCadenceNote(&store.Schedule{}); got != "" {
		t.Fatalf("a row with no delay, interval or cron has nothing to state: %q", got)
	}
	if got := scheduleCadenceNote(nil); got != "" {
		t.Fatalf("nil row: %q", got)
	}
}

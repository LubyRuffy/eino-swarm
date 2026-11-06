package remote

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A phone must be able to answer the same paused turn the desktop shows,
// including after reconnecting without having seen the original live event.
func TestPhoneCanContinueOrDeclineTheManagerCap(t *testing.T) {
	for _, proceed := range []bool{true, false} {
		t.Run(fmt.Sprint(proceed), func(t *testing.T) {
			e := testEngine(t)
			e.Config().Swarm.ManagerMaxIterations = 1
			th, err := e.CreateThread("manager cap", "", "")
			if err != nil {
				t.Fatal(err)
			}
			pump, log := testPump(t, e)
			turn, err := e.StartTurn(th.ID, "Inspect the assigned workspace.")
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool { return e.Status(th.ID).AwaitingContinue }, "manager cap")
			waitFor(t, func() bool {
				events, err := e.Store().ListEvents(th.ID, 0, 100)
				if err != nil {
					t.Fatal(err)
				}
				for _, ev := range events {
					if ev.Kind == engine.KindMaxIterations {
						return true
					}
				}
				return false
			}, "durable manager cap")

			pump.Dispatch([]byte(fmt.Sprintf(`{"v":1,"id":"watch-cap","op":"watch","thread_id":%q,"include_detail":true}`, th.ID)))
			ready := log.waitID(t, "watch-cap", time.Second)
			if !ready.OK || ready.Status == nil || !wireAwaitingContinue(t, ready.Status) || ready.Status.TurnID != turn.ID {
				t.Fatalf("reconnected phone lost its paused status: %+v", ready)
			}
			if ready.Detail == nil || ready.Detail.Running == nil || !wireAwaitingContinue(t, ready.Detail.Running) {
				t.Fatalf("open detail lost its paused status: %+v", ready.Detail)
			}
			foundCap := false
			for _, ev := range ready.Events {
				foundCap = foundCap || ev.Kind == engine.KindMaxIterations && ev.TurnID == turn.ID
			}
			if !foundCap {
				t.Fatalf("watch replay omitted the cap: %+v", ready.Events)
			}
			listed := Handle(e, config.RemoteConfig{}, Request{Op: OpList}, "relay", "session")
			if !listed.OK || len(listed.Running) != 1 || !wireAwaitingContinue(t, listed.Running[0]) {
				t.Fatalf("inbox lost its paused status: %+v", listed)
			}

			pump.Dispatch([]byte(fmt.Sprintf(`{"v":1,"id":"answer-cap","op":"continue","thread_id":%q,"continue":%t}`, th.ID, proceed)))
			if response := log.waitID(t, "answer-cap", time.Second); !response.OK {
				t.Fatalf("phone decision refused: %+v", response)
			}
			if proceed {
				waitFor(t, func() bool {
					events, err := e.Store().ListEvents(th.ID, 0, 200)
					if err != nil {
						t.Fatal(err)
					}
					continued := false
					for _, ev := range events {
						continued = continued || ev.Kind == engine.KindMaxIterationsContinued
						if continued && ev.Kind == "generation_started" && ev.AgentID == "manager" {
							return true
						}
					}
					return false
				}, "a real manager generation after phone Continue")
				// Each Continue grants one configured slice. Stop after proving that
				// slice ran so later mock slices do not leave a paused fixture behind.
				pump.Dispatch([]byte(fmt.Sprintf(`{"v":1,"id":"stop-cap","op":"stop","thread_id":%q}`, th.ID)))
				if response := log.waitID(t, "stop-cap", time.Second); !response.OK && response.Code != "idle" {
					t.Fatalf("phone Stop refused: %+v", response)
				}
			}
			var finished *store.Turn
			waitFor(t, func() bool {
				finished, err = e.Store().GetTurn(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				return !e.Status(th.ID).Running && finished.Status != store.TurnRunning
			}, "phone decision completing the original turn")
			if finished.Status != store.TurnCancelled && !(proceed && finished.Status == store.TurnDone) || e.Status(th.ID).AwaitingContinue {
				t.Fatalf("decision did not settle the original pause: %+v", finished)
			}
			if proceed {
				waitFor(t, func() bool {
					for _, response := range log.snapshot() {
						if response.Event != nil && response.Event.Kind == engine.KindMaxIterationsContinued && response.Event.TurnID == turn.ID {
							return true
						}
					}
					return false
				}, "live phone continuation receipt")
			}
			pump.Dispatch([]byte(fmt.Sprintf(`{"v":1,"id":"settled-cap","op":"continue","thread_id":%q,"continue":true}`, th.ID)))
			if response := log.waitID(t, "settled-cap", time.Second); response.OK || response.Code != "idle" {
				t.Fatalf("settled pause accepted again: %+v", response)
			}
		})
	}
}

func wireAwaitingContinue(t *testing.T, value any) bool {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields["awaiting_continue"] == true
}

func TestPhoneContinuationRequiresAnExplicitDecisionAndAPausedTurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("continuation validation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	for i, decision := range []string{"", `,"continue":null`, `,"continue":"true"`} {
		id := fmt.Sprintf("bad-cap-%d", i)
		pump.Dispatch([]byte(fmt.Sprintf(`{"v":1,"id":%q,"op":"continue","thread_id":%q%s}`, id, th.ID, decision)))
		responses := log.snapshot()
		response := responses[len(responses)-1]
		if response.OK || response.Code != "bad_request" {
			t.Fatalf("missing/invalid decision accepted: %+v", response)
		}
	}
	for _, tid := range []string{th.ID, "missing"} {
		var req Request
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"op":"continue","thread_id":%q,"continue":true}`, tid)), &req); err != nil {
			t.Fatal(err)
		}
		response := Handle(e, config.RemoteConfig{}, req, "relay", "session")
		wantCode := "idle"
		if tid == "missing" {
			wantCode = "not_found"
		}
		if response.OK || response.Code != wantCode {
			t.Fatalf("invalid target accepted: %+v", response)
		}
	}
	if _, err := e.StartTurn(th.ID, "Inspect the workspace."); err != nil {
		t.Fatal(err)
	}
	var req Request
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"op":"continue","thread_id":%q,"continue":true}`, th.ID)), &req); err != nil {
		t.Fatal(err)
	}
	if response := Handle(e, config.RemoteConfig{}, req, "relay", "session"); response.OK || response.Code != "idle" {
		t.Fatalf("running turn without a pause accepted continuation: %+v", response)
	}
}

// Absence means an older PC did not report cap status. A current PC must send
// false explicitly so a stale replayed cap cannot remain actionable.
func TestPhoneSnapshotSendsFalseWhenNoContinuationIsPending(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("idle cap status", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []any{watchStatus(e, th.ID, e.Config().Remote), runningView(e, th, 40)} {
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if status, exists := fields["awaiting_continue"]; !exists || string(status) != "false" {
			t.Fatalf("non-paused snapshot omitted its authoritative false status: %s", raw)
		}
	}
}

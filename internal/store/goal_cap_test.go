package store

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestGoalCapAndItsDistinctReasonCommitTogether(t *testing.T) {
	for _, failure := range []string{"", "event", "flag", "thread-query", "turn-query", "sequence", "deleted-thread", "deleted-turn"} {
		t.Run(failure, func(t *testing.T) {
			s := open(t)
			now := time.Now().UTC()
			th := &Thread{Goal: "complete assigned work", GoalStartedAt: &now, GoalAutoTurns: 2}
			if err := s.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			turn := &Turn{ThreadID: th.ID, GoalContinue: true}
			if err := s.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if failure == "deleted-thread" {
				if err := s.DeleteThread(th.ID); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "deleted-turn" {
				if err := s.DB().Delete(&Turn{}, "id = ?", turn.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if failure == "event" || failure == "flag" {
				sql := "CREATE TRIGGER refuse_goal_cap BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'cap receipt unavailable'); END"
				if failure == "flag" {
					sql = "CREATE TRIGGER refuse_goal_cap BEFORE UPDATE OF goal_capped ON threads BEGIN SELECT RAISE(ABORT, 'cap flag unavailable'); END"
				}
				if err := s.DB().Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			if failure == "thread-query" || failure == "turn-query" {
				if err := s.DB().Callback().Query().Before("gorm:query").Register("refuse-cap-read", func(tx *gorm.DB) {
					if failure == "thread-query" && tx.Statement.Table == "threads" || failure == "turn-query" && tx.Statement.Table == "turns" {
						tx.AddError(errors.New("cap identity unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "sequence" {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ev := &Event{Kind: "goal_capped", AgentID: "manager", Text: `{"reason":"schedule_claim_unverified","code":"no_matching_schedule","attempt":3,"cap":3}`}
			changed, err := s.CapGoalAfterTurn(GoalTurnScope{ThreadID: th.ID, TurnID: turn.ID, Goal: th.Goal, GoalStartedAt: &now}, 2, ev)
			if failure != "" {
				if err == nil || changed {
					t.Fatalf("cap refusal was ignored: changed=%v err=%v", changed, err)
				}
				if failure == "sequence" || failure == "thread-query" || failure == "deleted-thread" {
					return
				}
				got, err := s.GetThread(th.ID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := s.ListEvents(th.ID, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				if got.GoalCapped || len(events) != 0 {
					t.Fatalf("cap or reason survived transaction rollback: thread=%+v events=%+v", got, events)
				}
				return
			}
			got, err := s.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := s.ListEvents(th.ID, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err != nil || !changed || !got.GoalCapped || got.GoalBlocked || got.GoalAutoTurns != 2 || len(events) != 1 || events[0].Text != ev.Text || ev.Seq == 0 || ev.CreatedAt.IsZero() || ev.ThreadID != th.ID || ev.TurnID != turn.ID {
				t.Fatalf("cap and receipt differ: changed=%v err=%v thread=%+v events=%+v", changed, err, got, events)
			}
		})
	}
}

func TestStaleGoalCapCannotPauseNewHumanWork(t *testing.T) {
	for _, change := range []string{"goal", "completed", "blocked", "capped", "idle", "budget", "newer-turn"} {
		t.Run(change, func(t *testing.T) {
			s := open(t)
			now := time.Now().UTC()
			th := &Thread{Goal: "complete assigned work", GoalStartedAt: &now, GoalAutoTurns: 2}
			if err := s.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			turn := &Turn{ThreadID: th.ID}
			if err := s.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			fields := map[string]any{}
			switch change {
			case "goal":
				fields["goal"] = "replacement work"
			case "completed":
				fields["goal_complete"] = true
			case "blocked":
				fields["goal_blocked"] = true
			case "capped":
				fields["goal_capped"] = true
			case "idle":
				fields["goal_idle"] = true
			case "budget":
				fields["goal_auto_turns"] = 0
			case "newer-turn":
				if err := s.CreateTurn(&Turn{ThreadID: th.ID}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.UpdateThread(th.ID, fields); err != nil {
				t.Fatal(err)
			}
			changed, err := s.CapGoalAfterTurn(GoalTurnScope{ThreadID: th.ID, TurnID: turn.ID, Goal: th.Goal, GoalStartedAt: &now}, 2, &Event{Kind: "goal_capped"})
			if err != nil || changed {
				t.Fatalf("stale cap changed current goal: changed=%v err=%v", changed, err)
			}
			events, err := s.ListEvents(th.ID, 0, 0)
			if err != nil || len(events) != 0 {
				t.Fatalf("stale cap was published: events=%+v err=%v", events, err)
			}
		})
	}
}

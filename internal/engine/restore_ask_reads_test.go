package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Recovering a human wait already has a validated call from the shared
// snapshot. Reopening the complete turn here rereads its large checkpoints.
func TestRestoreBindsTheOriginalQuestionWithoutRereadingItsTurn(t *testing.T) {
	for _, action := range []string{"answer", "stop"} {
		t.Run(action, func(t *testing.T) {
			e, endpoint := newRestartReplayEngine(t)
			thread, err := e.CreateThread("Question snapshot recovery", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, thread.ID, "Continue after the decision.")
			args := restartReplayAskArguments(t, "decision")
			recordRestartReplayAsk(e, turn, "snapshot-ask", args)
			body, err := json.Marshal(restartCheckpoint{Version: 1, Pause: "ask", AskCallID: "snapshot-ask", ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText), restartReplayAskMessage("snapshot-ask", args)}})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.AppendEvent(&store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)}); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			observing := true
			checkpointReads, filteredReads, fullTurnReads := 0, 0, 0
			read := func(db *gorm.DB) {
				if db.Statement.Table != "events" {
					return
				}
				sql := db.Statement.SQL.String()
				mu.Lock()
				defer mu.Unlock()
				if !observing {
					return
				}
				if strings.Contains(sql, "kind =") && strings.Contains(sql, "ORDER BY seq desc") {
					checkpointReads++
				}
				if strings.Contains(sql, "AND (kind IN") {
					filteredReads++
				}
				if strings.Contains(sql, "turn_id =") && !strings.Contains(sql, "kind") {
					fullTurnReads++
				}
			}
			// This private Store retains callbacks until Close; callback removal can
			// race the resumed manager's normal post-answer work.
			if err := e.store.DB().Callback().Query().After("gorm:query").Register("question_snapshot_reads", read); err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Callback().Row().After("gorm:row").Register("question_snapshot_reads", read); err != nil {
				t.Fatal(err)
			}
			if err := e.Restore(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer })
			mu.Lock()
			cp, filtered, full := checkpointReads, filteredReads, fullTurnReads
			mu.Unlock()
			if cp != 1 || filtered != 1 || full != 0 {
				t.Errorf("restored question repeated its snapshot: checkpoints=%d filtered=%d full_turn_reads=%d", cp, filtered, full)
			}
			if len(endpoint.requests()) != 0 {
				t.Fatal("recovery called the manager before the original human answer")
			}
			if action == "stop" {
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				finished := waitRestartReplayTurn(t, e, turn.ID)
				mu.Lock()
				observing = false
				cancelReads := fullTurnReads
				mu.Unlock()
				if finished.Status != store.TurnCancelled || cancelReads != 0 || len(endpoint.requests()) != 0 {
					t.Fatalf("cancelled recovered question lost identity/no-read boundary: turn=%+v reads=%d calls=%d", finished, cancelReads, len(endpoint.requests()))
				}
				return
			}
			mu.Lock()
			observing = false
			mu.Unlock()
			if err := e.AnswerTurn(thread.ID, "wrong-question", AskAnswers{"decision": {Answers: []string{"one"}}}); !errors.Is(err, ErrAskMismatch) {
				t.Fatalf("wrong call was accepted: %v", err)
			}
			if err := e.AnswerTurn(thread.ID, "snapshot-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
				t.Fatal(err)
			}
			finished := waitRestartReplayTurn(t, e, turn.ID)
			if finished.ID != turn.ID || finished.Status != store.TurnDone || finished.Final != restartReplayFinal {
				t.Fatalf("original question did not finish: %+v", finished)
			}
			assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "snapshot-ask", "decision", "one")
			if err := e.AnswerTurn(thread.ID, "snapshot-ask", AskAnswers{"decision": {Answers: []string{"two"}}}); !errors.Is(err, ErrIdle) {
				t.Fatalf("completed question accepted another answer: %v", err)
			}
		})
	}
}

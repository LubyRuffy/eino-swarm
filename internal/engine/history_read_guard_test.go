package engine

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/history"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

type historyReadSpy struct {
	calls  atomic.Int32
	before func()
	ctx    context.Context
}

func (s *historyReadSpy) Query(ctx context.Context, _ string, _ []any, _ int) (history.Result, error) {
	s.calls.Add(1)
	s.ctx = ctx
	if s.before != nil {
		s.before()
	}
	return history.Result{Columns: []string{"original"}, Rows: [][]any{{"complete original\x00"}}, RowCount: 1}, nil
}

func guardedHistoryFixture(t *testing.T) (*Engine, *store.Turn, *historyQueryTool, *historyReadSpy, context.Context) {
	t.Helper()
	e := newTestEngine(t)
	thread, err := e.CreateThread("Guarded original read", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, thread.ID, store.Turn{})
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range set.Tools {
		if query, ok := base.(*historyQueryTool); ok {
			spy := &historyReadSpy{}
			query.reader = spy
			return e, turn, query, spy, withHistoryReadOwner(t.Context(), thread.ID, turn.ID)
		}
	}
	t.Fatal("production history tool is not mounted")
	return nil, nil, nil, nil, nil
}

func TestHistoryReadPersistsItsRealOwnerAndCompleteArgumentsBeforeReading(t *testing.T) {
	e, turn, query, spy, ctx := guardedHistoryFixture(t)
	args := `{"sql":"SELECT ? AS original","params":["retain the full condition\\u0000"]}`
	spy.before = func() {
		seen, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID)
		if err != nil || !seen {
			t.Errorf("original read ran before durable history mode: %v %v", seen, err)
		}
	}
	out, err := query.InvokableRun(ctx, args)
	if err != nil || spy.calls.Load() != 1 || !strings.Contains(out, "complete original") {
		t.Fatalf("guarded query=%q calls=%d err=%v", out, spy.calls.Load(), err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "history_read_started" || events[0].ThreadID != turn.ThreadID || events[0].TurnID != turn.ID || events[0].Text != args || events[0].ToolCallID != "" {
		t.Fatalf("durable start receipt changed owner/arguments or pretended completion: %#v %v", events, err)
	}
	// A new Engine only has persisted state. The read start alone still proves
	// the read when the best-effort ordinary tool-call receipt is missing; a
	// read without an explicit request never selects the report protocol.
	next := &Engine{store: e.store}
	required, queried, failure := next.historyAnswerMode(turn)
	if failure != nil || required || !queried {
		t.Fatalf("durable start mode was lost by a new engine: %v %v %+v", required, queried, failure)
	}
}

func TestHistoryReadGuardRejectsUntrustedMissingAndDeletedOwnersBeforeSQL(t *testing.T) {
	for _, reason := range []string{"missing", "empty-thread", "empty-turn", "bound-mismatch", "turn-mismatch", "unknown-turn", "deleted-turn", "deleted-thread", "dropped-turn"} {
		t.Run(reason, func(t *testing.T) {
			e, turn, query, spy, ctx := guardedHistoryFixture(t)
			switch reason {
			case "missing":
				ctx = t.Context()
			case "empty-thread":
				ctx = withHistoryReadOwner(ctx, "", turn.ID)
			case "empty-turn":
				ctx = withHistoryReadOwner(ctx, turn.ThreadID, "")
			case "bound-mismatch":
				query.context.ThreadID = "unrelated-thread"
			case "turn-mismatch":
				other, err := e.CreateThread("Other owner", "", "")
				if err != nil {
					t.Fatal(err)
				}
				otherTurn := mustStoredTurn(t, e, other.ID, store.Turn{})
				ctx = withHistoryReadOwner(ctx, turn.ThreadID, otherTurn.ID)
			case "unknown-turn":
				ctx = withHistoryReadOwner(ctx, turn.ThreadID, "missing-owner")
			case "deleted-turn":
				if err := e.store.DB().Delete(turn).Error; err != nil {
					t.Fatal(err)
				}
			case "deleted-thread":
				if err := e.store.DeleteThread(turn.ThreadID); err != nil {
					t.Fatal(err)
				}
			case "dropped-turn":
				e.recordMu.Lock()
				e.droppedTurns[turn.ID] = struct{}{}
				e.recordMu.Unlock()
			}
			if out, err := query.InvokableRun(ctx, `{}`); err == nil || out != "" || spy.calls.Load() != 0 {
				t.Fatalf("invalid owner read original history: out=%q calls=%d err=%v", out, spy.calls.Load(), err)
			}
		})
	}
}

func TestHistoryReadMarkerFailureIsAFatalStorageErrorBeforeSQL(t *testing.T) {
	for _, stage := range []string{"owner-read", "start-write", "closed-store"} {
		t.Run(stage, func(t *testing.T) {
			e, turn, query, spy, ctx := guardedHistoryFixture(t)
			cause := errors.New("fixture history " + stage + " failed")
			buffer := newManagerAnswerBuffer(newAccumulator(e, turn.ThreadID, turn.ID, 0))
			ctx = context.WithValue(ctx, scheduleAnswerBufferContextKey{}, buffer)
			switch stage {
			case "owner-read":
				if err := e.store.DB().Callback().Query().Before("gorm:query").Register("history_read_owner_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "turns" {
						_ = tx.AddError(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "start-write":
				if err := e.store.DB().Callback().Create().Before("gorm:create").Register("history_read_start_failure", func(tx *gorm.DB) {
					if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_read_started" {
						_ = tx.AddError(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "closed-store":
				if err := e.store.Close(); err != nil {
					t.Fatal(err)
				}
			}
			out, err := query.InvokableRun(ctx, `{}`)
			failure := buffer.reportError()
			if err == nil || out != "" || spy.calls.Load() != 0 || failure == nil || isHistoryAnswerCorrection(failure) {
				t.Fatalf("storage refusal allowed reading or a model correction: out=%q calls=%d err=%v failure=%+v", out, spy.calls.Load(), err, failure)
			}
			if stage != "closed-store" && (!errors.Is(err, cause) || !errors.Is(failure, cause)) {
				t.Fatalf("actual storage cause was lost: %v %+v", err, failure)
			}
		})
	}
}

func TestHistoryReadCancelledOwnerDoesNotReadOrCreateAStartReceipt(t *testing.T) {
	e, turn, query, spy, ctx := guardedHistoryFixture(t)
	ctx, cancel := context.WithCancelCause(ctx)
	cause := errors.New("human canceled original read")
	cancel(cause)
	if out, err := query.InvokableRun(ctx, `{}`); out != "" || !errors.Is(err, cause) || spy.calls.Load() != 0 {
		t.Fatalf("canceled owner read: %q %v calls=%d", out, err, spy.calls.Load())
	}
	if seen, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID); err != nil || seen {
		t.Fatalf("canceled request invented a start receipt: %v %v", seen, err)
	}
}

func TestHistoryReadKeepsTheOriginalTerminalOwnerOfALegitimateWorker(t *testing.T) {
	e, turn, query, spy, ctx := guardedHistoryFixture(t)
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	// A later manager check must not steal this worker's original owner ID.
	next := mustStoredTurn(t, e, turn.ThreadID, store.Turn{})
	if out, err := query.InvokableRun(ctx, `{}`); err != nil || out == "" || spy.calls.Load() != 1 {
		t.Fatalf("legitimate worker was tied to manager terminal/current turn: %q %v calls=%d", out, err, spy.calls.Load())
	}
	if seen, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID); err != nil || !seen {
		t.Fatalf("worker original ownership lost: %v %v", seen, err)
	}
	if seen, err := e.store.TurnHasHistoryQuery(next.ThreadID, next.ID); err != nil || seen {
		t.Fatalf("worker was silently rebound to an unrelated new manager turn: %v %v", seen, err)
	}
}

func TestActualHistoryReadWithMissingOrdinaryCallReceiptStillUsesHostRendering(t *testing.T) {
	e, thread, _, managers, _ := newHistoryPolicyFixture(t, nil)
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("history_ordinary_call_receipt_failure", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "tool_call" && strings.HasPrefix(event.Text, ToolQueryHistory+"(") {
			_ = tx.AddError(errors.New("fixture ordinary call receipt unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !strings.Contains(got.Final, historyRecoveryOriginal) || strings.Contains(got.Final, `"version":1`) || managers.Load() != 2 {
		t.Fatalf("missing best-effort call receipt bypassed durable report mode: %+v managers=%d", got, managers.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, event := range events {
		if event.Kind == "history_read_started" {
			starts++
			if event.ToolCallID != "original-history-call" || event.Text != `{}` {
				t.Fatalf("actual adapter lost call identity/raw arguments: %+v", event)
			}
		}
	}
	if starts != 1 {
		t.Fatalf("actual original read start receipts=%d", starts)
	}
}

func TestActualHistoryReadStartStorageFailureStopsBeforeAnotherManagerRequest(t *testing.T) {
	e, thread, _, managers, _ := newHistoryPolicyFixture(t, nil)
	cause := errors.New("fixture durable history start unavailable")
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("history_durable_start_refused", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_read_started" {
			_ = tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnError || got.Final != "" || managers.Load() != 1 {
		t.Fatalf("start storage failure repeated work or published generated prose: %+v requests=%d", got, managers.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "agent_message" || event.Kind == "history_read_started" || event.Kind == "tool_result" && strings.Contains(event.Text, historyRecoveryOriginal) {
			t.Fatalf("refused start exposed original history or invented a receipt: %+v", event)
		}
	}
}

func TestHistoryReadCancellationWhileWaitingForTheStoreKeepsItsCause(t *testing.T) {
	e, _, query, spy, ctx := guardedHistoryFixture(t)
	sqlDB, err := e.store.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, runErr := query.InvokableRun(ctx, `{}`)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(runErr, context.DeadlineExceeded) || spy.calls.Load() != 0 {
		t.Fatalf("owner store lookup ignored caller cancellation: %v calls=%d", runErr, spy.calls.Load())
	}
}

type observedHistoryOwnerContext struct {
	context.Context
	observed chan struct{}
	once     *sync.Once
}

func (c observedHistoryOwnerContext) Value(key any) any {
	value := c.Context.Value(key)
	if _, ok := key.(historyReadOwnerKey); ok {
		c.once.Do(func() { close(c.observed) })
	}
	return value
}

func TestHistoryReadCancellationWhileWaitingForPublicationDoesNotRead(t *testing.T) {
	e, _, query, spy, ctx := guardedHistoryFixture(t)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	observed := make(chan struct{})
	ctx = observedHistoryOwnerContext{Context: ctx, observed: observed, once: &sync.Once{}}
	e.recordMu.Lock()
	done := make(chan error, 1)
	go func() { _, err := query.InvokableRun(ctx, `{}`); done <- err }()
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		cancel(context.Canceled)
		e.recordMu.Unlock()
		<-done
		t.Fatal("history guard never selected the trusted owner")
	}
	cause := errors.New("human canceled while publication ownership was occupied")
	cancel(cause)
	e.recordMu.Unlock()
	if err := <-done; !errors.Is(err, cause) || spy.calls.Load() != 0 {
		t.Fatalf("cancelled queued guard read: %v calls=%d", err, spy.calls.Load())
	}
}

func TestHistoryReadCancellationAtStoreBoundariesNeverReadsOriginals(t *testing.T) {
	for _, stage := range []string{"owner-read", "start-write"} {
		t.Run(stage, func(t *testing.T) {
			e, turn, query, spy, ctx := guardedHistoryFixture(t)
			ctx, cancel := context.WithCancelCause(ctx)
			defer cancel(context.Canceled)
			cause := errors.New("human canceled at " + stage)
			if stage == "owner-read" {
				if err := e.store.DB().Callback().Query().After("gorm:query").Register("history_read_owner_cancellation", func(tx *gorm.DB) {
					if tx.Statement.Table == "turns" {
						cancel(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else if err := e.store.DB().Callback().Create().After("gorm:create").Register("history_read_start_cancellation", func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindHistoryReadStarted {
					cancel(cause)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if out, err := query.InvokableRun(ctx, `{}`); out != "" || !errors.Is(err, cause) || spy.calls.Load() != 0 {
				t.Fatalf("cancelled store boundary read: %q %v calls=%d", out, err, spy.calls.Load())
			}
			seen, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID)
			if err != nil || seen != (stage == "start-write") {
				t.Fatalf("start receipt pretended more than actual write: %v %v", seen, err)
			}
		})
	}
}

func TestHistoryReadGuardAndOriginalSQLShareOneDeadline(t *testing.T) {
	_, _, query, spy, ctx := guardedHistoryFixture(t)
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	original := query.guard
	var guardContext context.Context
	query.guard = func(ctx context.Context, threadID, args string) error {
		guardContext = ctx
		return original(ctx, threadID, args)
	}
	if _, err := query.InvokableRun(ctx, `{}`); err != nil {
		t.Fatal(err)
	}
	if guardContext != spy.ctx {
		t.Fatal("durable guard and SQL reader were given different deadline contexts")
	}
	want, _ := ctx.Deadline()
	if got, ok := spy.ctx.Deadline(); !ok || !got.Equal(want) {
		t.Fatalf("caller deadline was replaced instead of sharing the earlier budget: %v %v", got, ok)
	}
}

func TestHistoryReadCancellationWhileWaitingToWriteItsMarkerPreventsSQL(t *testing.T) {
	e, turn, query, spy, ctx := guardedHistoryFixture(t)
	// Warm only the existing seq cache. Its legacy cold lookup deliberately
	// remains outside the new write-context API and has separate semantics.
	if err := e.store.AppendEvent(&store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "fixture_seq_seed"}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := e.store.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	var held *sql.Conn
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("history_marker_writer_wait", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindHistoryReadStarted {
			var err error
			held, err = sqlDB.Conn(t.Context())
			if err != nil {
				_ = tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, runErr := query.InvokableRun(ctx, `{}`)
	if held != nil {
		if err := held.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(runErr, context.DeadlineExceeded) || spy.calls.Load() != 0 || held == nil {
		t.Fatalf("marker writer wait ignored the shared SQL deadline: %v calls=%d held=%v", runErr, spy.calls.Load(), held != nil)
	}
	if seen, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID); err != nil || seen {
		t.Fatalf("canceled marker wait invented durable history access: %v %v", seen, err)
	}
}

type excludedHistoryWorkerValueKey struct{}

func TestHistoryWorkerContextCopiesOnlyItsOwnerAndPrivateFailureSink(t *testing.T) {
	e, turn, query, spy, parent := guardedHistoryFixture(t)
	base := context.WithValue(t.Context(), excludedHistoryWorkerValueKey{}, "detached host value")
	if got := historyWorkerContextValues(context.Background(), base); got != base {
		t.Fatal("absent history owner replaced the detached base")
	}
	buffer := newManagerAnswerBuffer(newAccumulator(e, turn.ThreadID, turn.ID, 0))
	parent = context.WithValue(parent, scheduleAnswerBufferContextKey{}, buffer)
	parent = context.WithValue(parent, excludedHistoryWorkerValueKey{}, "manager-only accounting/role state")
	child := historyWorkerContextValues(parent, context.Background())
	owner, ok := child.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok || owner.ThreadID != turn.ThreadID || owner.TurnID != turn.ID || owner.failureSink != buffer || child.Value(excludedHistoryWorkerValueKey{}) != nil || child.Value(scheduleAnswerBufferContextKey{}) != nil {
		t.Fatalf("worker inherited more than its selected private identity: owner=%+v excluded=%v", owner, child.Value(excludedHistoryWorkerValueKey{}))
	}
	cause := errors.New("fixture worker start write failed")
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("history_worker_start_failure", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindHistoryReadStarted {
			_ = tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := query.InvokableRun(child, `{}`); !errors.Is(err, cause) || spy.calls.Load() != 0 || !errors.Is(buffer.reportError(), cause) {
		t.Fatalf("selected worker failure sink lost actual storage refusal: err=%v calls=%d failure=%+v", err, spy.calls.Load(), buffer.reportError())
	}
}

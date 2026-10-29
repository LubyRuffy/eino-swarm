package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/history"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/compose"
	"gorm.io/gorm"
)

// KindHistoryReadStarted records admission to a history read, not its completion.
const KindHistoryReadStarted = "history_read_started"

type historyReadOwnerKey struct{}

type historyReadOwner struct {
	ThreadID    string
	TurnID      string
	failureSink *managerAnswerBuffer
}

type historyQueryReader interface {
	Query(context.Context, string, []any, int) (history.Result, error)
}

// The owner is injected by the host at actual manager execution. Workers
// inherit that identity; model arguments cannot select a different turn.
func withHistoryReadOwner(ctx context.Context, threadID, turnID string) context.Context {
	return context.WithValue(ctx, historyReadOwnerKey{}, historyReadOwner{ThreadID: threadID, TurnID: turnID})
}

func historyWorkerContextValues(parent, detached context.Context) context.Context {
	owner, ok := parent.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok {
		return detached
	}
	if buffer, _ := parent.Value(scheduleAnswerBufferContextKey{}).(*managerAnswerBuffer); buffer != nil {
		owner.failureSink = buffer
	}
	return context.WithValue(detached, historyReadOwnerKey{}, owner)
}

func (e *Engine) admitHistoryRead(ctx context.Context, boundThreadID, args string) (err error) {
	defer func() {
		if err == nil {
			return
		}
		if cause := context.Cause(ctx); cause != nil {
			err = errors.Join(err, cause)
		}
		failure := &scheduleAnswerError{Code: "history_read_unavailable", Cause: err}
		buffer, _ := ctx.Value(scheduleAnswerBufferContextKey{}).(*managerAnswerBuffer)
		if buffer == nil {
			owner, _ := ctx.Value(historyReadOwnerKey{}).(historyReadOwner)
			buffer = owner.failureSink
		}
		if buffer != nil {
			buffer.reportStatus(failure)
		}
		err = failure
	}()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	owner, ok := ctx.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok || owner.ThreadID == "" || owner.TurnID == "" || owner.ThreadID != boundThreadID {
		return fmt.Errorf("original history read has no matching trusted turn owner")
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if _, deleted := e.droppedTurns[owner.TurnID]; deleted {
		return store.ErrNotFound
	}
	// A retained worker may legitimately outlive its manager check. Require
	// its original stored owner, without stealing a newer runtime turn ID.
	var turn store.Turn
	if err := e.store.DB().WithContext(ctx).Select("id", "thread_id").Where("id=?", owner.TurnID).First(&turn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.ErrNotFound
		}
		return err
	}
	if turn.ThreadID != owner.ThreadID {
		return fmt.Errorf("original history read owner belongs to another conversation")
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	event := store.Event{ThreadID: owner.ThreadID, TurnID: owner.TurnID, Kind: KindHistoryReadStarted, ToolCallID: compose.GetToolCallID(ctx), Text: args}
	if err := e.store.AppendEventContext(ctx, &event); err != nil {
		return fmt.Errorf("persist original history read start: %w", err)
	}
	e.broadcast(event)
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return nil
}

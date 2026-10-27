package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const KindWorkersCollectionResumed = "workers_collection_resumed"
const KindWorkersCollectionDeclined = "workers_collection_declined"

// The generation-bound wait is independent of the one correction allowance.
// Only its one durable completion receipt may admit an automatic attempt.
type workerCollectionPause struct {
	Reason          string               `json:"reason"`
	WaitID          string               `json:"wait_id"`
	Limit           int                  `json:"limit"`
	Extend          int                  `json:"extend_by"`
	WorkerIDs       []string             `json:"worker_ids"`
	Workers         []swarm.PendingAgent `json:"workers"`
	AutomaticResume bool                 `json:"automatic_resume"`
	Refusal         string               `json:"automatic_resume_refusal,omitempty"`
	Decision        *bool                `json:"human_decision,omitempty"`
	failure         error
}

type workerCollectionReceipt struct {
	WaitID    string               `json:"wait_id"`
	WorkerIDs []string             `json:"worker_ids"`
	Workers   []swarm.PendingAgent `json:"workers"`
}

func (e *Engine) workerCollectionWaitState(turn *store.Turn, snapshots ...*restoreSnapshot) (*workerCollectionPause, bool, error) {
	events, err := e.restoreTurnEvents(turn.ID, snapshots...)
	if err != nil {
		return nil, false, fmt.Errorf("read worker completion wait: %w", err)
	}
	return workerCollectionWaitEvents(events)
}

func validWorkerCollectionPause(p *workerCollectionPause) bool {
	if strings.TrimSpace(p.WaitID) != p.WaitID || p.WaitID == "" || p.Limit < 0 || p.Extend <= 0 || len(p.Workers) == 0 || len(p.Workers) != len(p.WorkerIDs) {
		return false
	}
	previous := ""
	for i, w := range p.Workers {
		if strings.TrimSpace(w.AgentID) != w.AgentID || w.AgentID == "" || w.AgentID <= previous || isManagerAgent(w.AgentID) || w.Generation <= 0 || w.AgentID != p.WorkerIDs[i] || p.AutomaticResume && !w.Running {
			return false
		}
		previous = w.AgentID
	}
	return true
}

func matchingWorkerCollectionReceipt(p *workerCollectionPause, receipt workerCollectionReceipt) bool {
	if p == nil || receipt.WaitID != p.WaitID || len(receipt.Workers) != len(p.Workers) || len(receipt.WorkerIDs) != len(p.WorkerIDs) {
		return false
	}
	for i, expected := range p.Workers {
		if receipt.WorkerIDs[i] != expected.AgentID || receipt.Workers[i].AgentID != expected.AgentID || receipt.Workers[i].Generation != expected.Generation {
			return false
		}
	}
	return true
}

func workerCollectionWaitEvents(events []store.Event) (*workerCollectionPause, bool, error) {
	var paused *workerCollectionPause
	used := false
	for _, ev := range events {
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		switch ev.Kind {
		case KindWorkersCollectionResumed:
			var receipt workerCollectionReceipt
			if json.Unmarshal([]byte(ev.Text), &receipt) != nil || used || !matchingWorkerCollectionReceipt(paused, receipt) || !paused.AutomaticResume || !workerCollectionReady(paused.Workers, receipt.Workers) || paused.Decision != nil {
				return nil, used, fmt.Errorf("invalid worker completion resume receipt")
			}
			used, paused = true, nil
		case KindWorkersCollectionDeclined:
			var receipt workerCollectionReceipt
			if json.Unmarshal([]byte(ev.Text), &receipt) != nil || !matchingWorkerCollectionReceipt(paused, receipt) {
				return nil, used, fmt.Errorf("invalid worker collection decline receipt")
			}
			no := false
			paused.Decision, paused.AutomaticResume = &no, false
		case KindMaxIterationsContinued:
			paused = nil // a human continuation does not renew the automatic wake
		case KindMaxIterations:
			var body workerCollectionPause
			if json.Unmarshal([]byte(ev.Text), &body) != nil {
				return nil, used, fmt.Errorf("invalid persisted manager pause")
			}
			if body.Reason != "workers_uncollected" {
				continue
			}
			if !validWorkerCollectionPause(&body) {
				return nil, used, fmt.Errorf("invalid persisted worker completion wait")
			}
			paused = &body
		}
	}
	return paused, used, nil
}

func (rt *runtime) newWorkerCollectionPause(turn *store.Turn, workers []swarm.PendingAgent, used, extend int) (*workerCollectionPause, error) {
	_, resumed, err := rt.engine.workerCollectionWaitState(turn)
	if err != nil {
		return nil, err
	}
	p := &workerCollectionPause{Reason: "workers_uncollected", WaitID: store.NewID("workerwait"), Limit: used, Extend: extend,
		Workers: workers, AutomaticResume: !resumed}
	for _, worker := range p.Workers {
		p.WorkerIDs = append(p.WorkerIDs, worker.AgentID)
		if !worker.Running || worker.Generation <= 0 {
			p.AutomaticResume = false
		}
	}
	if len(p.Workers) == 0 {
		p.AutomaticResume = false
	}
	return p, nil
}

func workerCollectionReady(expected, current []swarm.PendingAgent) bool {
	if len(expected) == 0 || len(expected) != len(current) {
		return false
	}
	for i, want := range expected {
		if !want.Running || want.Generation <= 0 || want.AgentID != current[i].AgentID || want.Generation != current[i].Generation || current[i].Running {
			return false
		}
	}
	return true
}

func (rt *runtime) saveWorkerPauseEvent(turn *store.Turn, kind string, value any) error {
	var body []byte
	if plain, ok := value.(string); ok {
		body = []byte(plain)
	} else {
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, deleted := e.droppedTurns[turn.ID]; deleted {
		return fmt.Errorf("worker completion wait owner was deleted")
	}
	if kind == KindWorkersCollectionResumed {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return fmt.Errorf("verify worker completion authorization: %w", err)
		}
		for _, ev := range events {
			if ev.Kind == KindUserStopRequested {
				rt.userStopped = true
				return context.Canceled
			}
		}
		paused, used, err := workerCollectionWaitEvents(events)
		receipt, ok := value.(workerCollectionReceipt)
		if err != nil {
			return err
		}
		if used || !ok || !matchingWorkerCollectionReceipt(paused, receipt) || !paused.AutomaticResume || paused.Decision != nil {
			return fmt.Errorf("worker completion authorization changed")
		}
	}
	ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: kind, Text: string(body)}
	if err := e.store.AppendEvent(&ev); err != nil {
		return fmt.Errorf("persist worker completion wait: %w", err)
	}
	e.broadcast(ev)
	return nil
}

// Renewing and consuming the single correction are one human decision. A
// crash after ACK must see exactly the same allowance as the live waiter.
func (rt *runtime) saveWorkerCollectionContinue(turn *store.Turn, p *workerCollectionPause) error {
	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, deleted := e.droppedTurns[turn.ID]; deleted {
		return fmt.Errorf("worker completion wait owner was deleted")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return fmt.Errorf("verify worker continuation authorization: %w", err)
	}
	for _, ev := range events {
		if ev.Kind == KindUserStopRequested {
			rt.userStopped = true
			return context.Canceled
		}
	}
	paused, _, err := workerCollectionWaitEvents(events)
	if err != nil {
		return err
	}
	if !matchingWorkerCollectionReceipt(paused, workerCollectionReceipt{p.WaitID, p.WorkerIDs, p.Workers}) || paused.Decision != nil {
		return fmt.Errorf("worker continuation authorization changed")
	}
	body, _ := json.Marshal(map[string]any{"attempt": 1, "cap": 1, "reason": "workers_uncollected", "worker_ids": p.WorkerIDs, "wait_id": p.WaitID})
	continued := &store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: KindMaxIterationsContinued, Text: fmt.Sprintf("continuing for another %d model rounds", p.Extend)}
	consumed := &store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: KindModelRetry, Text: string(body)}
	if err := e.store.AppendEventsAtomic([]*store.Event{continued, consumed}); err != nil {
		return fmt.Errorf("persist worker continuation decision: %w", err)
	}
	e.broadcast(*continued)
	e.broadcast(*consumed)
	return nil
}

// Only this pause observes worker completion. An ordinary iteration pause
// retains its explicit human Continue/Stop semantics.
func (rt *runtime) waitForWorkerCollection(ctx context.Context, turn *store.Turn, reg *swarm.Registry, p *workerCollectionPause) (proceed, automatic bool, err error) {
	_, consumed, err := rt.engine.workerCollectionWaitState(turn)
	if err != nil {
		return false, false, err
	}
	if consumed {
		p.AutomaticResume = false
	}
	ch := make(chan bool, 1)
	rt.mu.Lock()
	rt.continueCh, rt.acceptedContinue, rt.collectionPause = ch, nil, p
	if err = rt.saveWorkerPauseEvent(turn, KindMaxIterations, p); err != nil {
		rt.continueCh, rt.collectionPause = nil, nil
		rt.mu.Unlock()
		return false, false, err
	}
	// Steering can be acknowledged before this waiter exists. Admission and
	// steering share rt.mu, so consume that queued human continuation before
	// releasing the lock, using the same durable decision as ContinueTurn.
	queuedHuman := false
	if reg.HasPendingSteers() {
		for _, message := range reg.ManagerInbox() {
			if messageSource(message) == store.MessageSourceHuman && swarm.SteerSeq(message) > 0 {
				queuedHuman = true
				break
			}
		}
	}
	if queuedHuman {
		if err = rt.saveWorkerCollectionContinue(turn, p); err != nil {
			rt.continueCh, rt.collectionPause = nil, nil
			rt.mu.Unlock()
			return false, false, err
		}
		proceed := true
		p.Decision, p.AutomaticResume = &proceed, false
		rt.signalContinueLocked(true)
	}
	rt.mu.Unlock()
	defer func() { rt.mu.Lock(); rt.continueCh, rt.collectionPause = nil, nil; rt.mu.Unlock() }()
	var completed <-chan error
	if p.AutomaticResume {
		waitCtx, cancel := context.WithCancel(ctx)
		result := make(chan error, 1)
		joined := make(chan struct{})
		go func() { defer close(joined); result <- reg.WaitPendingCompletion(waitCtx, p.Workers) }()
		defer func() { cancel(); <-joined }()
		completed = result
	}
	for {
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case human := <-ch:
			rt.mu.Lock()
			failure := p.failure
			if human {
				if p.Decision == nil {
					err = rt.saveWorkerCollectionContinue(turn, p)
				}
			}
			rt.mu.Unlock()
			if failure != nil {
				return false, false, failure
			}
			if err != nil {
				return false, false, err
			}
			return human, false, nil
		case completionErr := <-completed:
			completed = nil
			rt.mu.Lock()
			if ctx.Err() != nil || rt.userStopped || rt.abandoned {
				rt.mu.Unlock()
				return false, false, ctx.Err()
			}
			if rt.acceptedContinue != nil {
				rt.mu.Unlock()
				continue
			}
			if completionErr != nil || !workerCollectionReady(p.Workers, reg.PendingAgents()) {
				p.AutomaticResume, p.Refusal = false, "worker_generation_changed"
				err = rt.saveWorkerPauseEvent(turn, KindMaxIterations, p)
				rt.mu.Unlock()
				if err != nil {
					return false, false, err
				}
				continue
			}
			value := workerCollectionReceipt{p.WaitID, p.WorkerIDs, reg.PendingAgents()}
			err = rt.saveWorkerPauseEvent(turn, KindWorkersCollectionResumed, value)
			p.AutomaticResume = false
			rt.mu.Unlock()
			if err != nil {
				return false, false, err
			}
			return true, true, nil
		}
	}
}

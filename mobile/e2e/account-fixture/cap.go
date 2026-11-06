package main

import (
	"fmt"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// capObservation contains only identities, durable event numbers and runtime
// state. The control route creates input; Continue and Stop belong to the UI's
// encrypted Pairlink RPC, never to this observer.
type capObservation struct {
	ThreadID                       string `json:"thread_id"`
	Title                          string `json:"title"`
	TurnID                         string `json:"turn_id"`
	Running                        bool   `json:"running"`
	AwaitingContinue               bool   `json:"awaiting_continue"`
	MaxIterationsSeq               int64  `json:"max_iterations_seq"`
	MaxIterationsContinuedSeq      int64  `json:"max_iterations_continued_seq"`
	ContinuedCount                 int    `json:"continued_count"`
	GenerationCount                int    `json:"generation_count"`
	ManagerGenerationAfterContinue bool   `json:"manager_generation_after_continue"`
	TurnStatus                     string `json:"turn_status"`
	TerminalSeq                    int64  `json:"terminal_seq"`
	TurnCount                      int    `json:"turn_count"`
}

// The caller holds f.mu, as for the other disposable fixture controls.
func (f *fixture) capRoute(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/cap/start" && r.URL.Path != "/api/cap/state" {
		return false
	}
	wantMethod := http.MethodGet
	if r.URL.Path == "/api/cap/start" {
		wantMethod = http.MethodPost
	}
	if r.Method != wantMethod {
		http.Error(w, "invalid cap fixture method", http.StatusMethodNotAllowed)
		return true
	}
	if f.engine == nil {
		http.Error(w, "cap fixture engine unavailable", http.StatusServiceUnavailable)
		return true
	}
	if r.URL.Path == "/api/cap/start" {
		if err := f.startCap(); err != nil {
			http.Error(w, "cap fixture input unavailable", http.StatusServiceUnavailable)
			return true
		}
	}
	observed, err := f.capState()
	if err != nil {
		http.Error(w, "cap fixture observation unavailable", http.StatusServiceUnavailable)
		return true
	}
	data(w, observed)
	return true
}

func (f *fixture) startCap() error {
	// The real engine reads this synchronized private configuration. It is
	// deliberately changed only by this opt-in fixture control, not ordinary
	// account scenarios, and no event or pending state is fabricated.
	if err := f.engine.Config().Update(func(cfg *config.Config) error {
		cfg.Swarm.ManagerMaxIterations = 1
		return nil
	}); err != nil {
		return err
	}
	title := fmt.Sprintf("Native cap %d", f.capStarts+1)
	thread, err := f.engine.CreateThread(title, "", "")
	if err != nil {
		return err
	}
	turn, err := f.engine.StartTurn(thread.ID, "Inspect the assigned workspace and report the findings.")
	if err != nil {
		return err
	}
	f.capStarts++
	f.capThreadID, f.capTurnID, f.capTitle = thread.ID, turn.ID, title
	return nil
}

func (f *fixture) capState() (capObservation, error) {
	observed := capObservation{ThreadID: f.capThreadID, TurnID: f.capTurnID, Title: f.capTitle}
	if f.capThreadID == "" {
		return observed, nil
	}
	turns, err := f.engine.Store().ListTurns(f.capThreadID)
	if err != nil {
		return observed, err
	}
	observed.TurnCount = len(turns)
	for _, turn := range turns {
		if turn.ID == f.capTurnID {
			observed.TurnStatus = turn.Status
		}
	}
	if observed.TurnStatus == "" {
		return observed, fmt.Errorf("cap fixture original turn unavailable")
	}
	events, err := f.engine.Store().ListEventsByTurn(f.capThreadID, f.capTurnID)
	if err != nil {
		return observed, err
	}
	for _, event := range events {
		if event.AgentID != "" && event.AgentID != "manager" {
			continue
		}
		switch event.Kind {
		case "max_iterations":
			observed.MaxIterationsSeq = event.Seq
		case "max_iterations_continued":
			observed.MaxIterationsContinuedSeq = event.Seq
			observed.ContinuedCount++
		case "generation_started":
			observed.GenerationCount++
			if observed.MaxIterationsContinuedSeq > 0 {
				observed.ManagerGenerationAfterContinue = true
			}
		case "done", "error":
			observed.TerminalSeq = event.Seq
		}
	}
	status := f.engine.Status(f.capThreadID)
	observed.Running, observed.AwaitingContinue = status.Running, status.AwaitingContinue
	return observed, nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

const (
	restartBudget     = 30 * time.Second
	handoffBudget     = 20 * time.Second
	quietPoll         = 200 * time.Millisecond
	replaceHeartbeat  = 5 * time.Second
	replaceForceAfter = handoffBudget
)

// These clocks let the legacy compatibility tests cover a long running turn
// without delaying every test by the entire handoff budget.
var quietNow = time.Now
var quietSleep = time.Sleep

func replaceEngine(ctx context.Context, rec lease.Record) error {
	log.Print("saving task progress before restarting the engine (30s budget)")
	handoff, cancel := context.WithTimeout(ctx, handoffBudget)
	legacy, err := requestEngineHandoff(handoff, rec.URL)
	cancel()
	if err != nil {
		return err
	}
	if legacy {
		log.Print("the older engine has no handoff support; waiting briefly before automatic recovery")
		if _, err := waitUntilQuiet(ctx, rec.URL); err != nil {
			return err
		}
		log.Print("restarting the older engine; recovering its recorded task progress")
	} else {
		log.Print("task progress saved; restarting the engine and restoring unfinished tasks")
	}
	owner := ctx
	if policy, ok := ctx.Value(engineStartupPolicyKey{}).(engineStartupPolicy); ok && !legacy {
		owner = policy.owner
	}
	if err := engineContextError(owner); err != nil {
		return fmt.Errorf("engine restart: %w", err)
	}
	return lease.Kill(rec.PID)
}

func requestEngineHandoff(ctx context.Context, base string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/engine/restart", nil)
	if err != nil {
		return false, fmt.Errorf("engine restart request: %w", err)
	}
	resp, err := handoffHTTP(req, replaceHeartbeat)
	if err != nil {
		return false, fmt.Errorf("engine handoff: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return true, nil
	}
	// Only a durable positive acknowledgement permits replacing a capable
	// engine. A timeout or failed store must not silently become a force kill.
	if resp.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&failure)
		detail := strings.Join(strings.Fields(failure.Error), " ")
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return false, fmt.Errorf("engine handoff failed (HTTP %d); task progress could not be confirmed: %s", resp.StatusCode, detail)
	}
	var status struct {
		Stage        string `json:"stage"`
		Ready        bool   `json:"ready"`
		Threads      int    `json:"threads"`
		Workers      int    `json:"workers"`
		Checkpointed int    `json:"checkpointed"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
		return false, fmt.Errorf("engine handoff response: %w", err)
	}
	if !status.Ready || status.Stage != "ready" {
		return false, fmt.Errorf("engine handoff is incomplete; task progress could not be confirmed")
	}
	log.Printf("saved %d conversation(s), %d/%d worker checkpoint(s)", status.Threads, status.Checkpointed, status.Workers)
	return false, nil
}

// The old engine can spend most of its budget in one pending tool. Keep the
// launching terminal informed without turning progress reads into new work.
func handoffHTTP(req *http.Request, heartbeat time.Duration) (*http.Response, error) {
	type response struct {
		value *http.Response
		err   error
	}
	done := make(chan response, 1)
	go func() { value, err := http.DefaultClient.Do(req); done <- response{value, err} }()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case result := <-done:
			return result.value, result.err
		case <-ticker.C:
			progress := req.Clone(req.Context())
			progress.Method, progress.Body = http.MethodGet, nil
			resp, err := client.Do(progress)
			if err != nil {
				continue
			}
			var status struct {
				Stage        string `json:"stage"`
				Workers      int    `json:"workers"`
				Checkpointed int    `json:"checkpointed"`
				Settled      int    `json:"settled"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&status)
			_ = resp.Body.Close()
			if err == nil && resp.StatusCode == http.StatusOK && status.Stage != "" {
				log.Printf("preparing restart: %s; %d/%d worker(s) settled; %d/%d checkpoint(s) saved", status.Stage, status.Settled, status.Workers, status.Checkpointed, status.Workers)
			}
		}
	}
}

type replaceWait struct {
	poll, heartbeat, forceAfter time.Duration
	now                         func() time.Time
	sleep                       func(time.Duration)
	logf                        func(string, ...any)
}

func waitUntilQuiet(ctx context.Context, base string) (bool, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	return waitToReplace(func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return engineBusy(client, base)
	}, replaceWait{poll: quietPoll, heartbeat: replaceHeartbeat, forceAfter: replaceForceAfter,
		now: quietNow, sleep: quietSleep, logf: log.Printf})
}

func waitToReplace(busy func() (bool, error), w replaceWait) (bool, error) {
	if w.now == nil {
		w.now = time.Now
	}
	if w.sleep == nil {
		w.sleep = time.Sleep
	}
	if w.logf == nil {
		w.logf = log.Printf
	}
	if w.poll <= 0 {
		w.poll = quietPoll
	}
	if w.forceAfter <= 0 {
		w.forceAfter = replaceForceAfter
	}
	started, lastBeat := w.now(), time.Duration(0)
	for {
		running, err := busy()
		if err != nil {
			return false, err
		}
		if !running {
			return false, nil
		}
		elapsed := w.now().Sub(started)
		if elapsed >= w.forceAfter {
			w.logf("restart handoff deadline reached; automatically recovering unfinished tasks")
			return true, nil
		}
		if w.heartbeat > 0 && elapsed-lastBeat >= w.heartbeat {
			w.logf("preparing restart: %s elapsed; waiting for the older engine", elapsed.Round(time.Second))
			lastBeat = elapsed
		}
		w.sleep(w.poll)
	}
}

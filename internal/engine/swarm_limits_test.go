package engine

import (
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestApplyLiveSwarmLimitsResizesParkedAndRunningRegistries(t *testing.T) {
	// PUT /settings used to rewrite yaml and leave the live / parked
	// registry on the cap minted at first spawn. Queued workers stayed
	// queued until a restart.
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	rt := e.runtimeFor(th.ID)
	live := swarm.NewRegistry()
	parked := swarm.NewRegistry()
	t.Cleanup(live.Close)
	t.Cleanup(parked.Close)
	live.MaxConcurrent = 1
	parked.MaxConcurrent = 1
	rt.mu.Lock()
	rt.reg = live
	rt.parked = parked
	rt.mu.Unlock()

	e.cfg.Swarm.MaxConcurrent = 9
	e.cfg.Swarm.MaxTurns = 11
	e.cfg.Swarm.AgentTimeoutSeconds = 13
	e.ApplyLiveSwarmLimits()

	if live.MaxConcurrent != 9 || parked.MaxConcurrent != 9 {
		t.Fatalf("cap not pushed live=%d parked=%d", live.MaxConcurrent, parked.MaxConcurrent)
	}
	if live.MaxTurns != 11 || parked.MaxTurns != 11 {
		t.Fatalf("turns not pushed live=%d parked=%d", live.MaxTurns, parked.MaxTurns)
	}
	wantTimeout := 13 * time.Second
	if live.AgentTimeout != wantTimeout || parked.AgentTimeout != wantTimeout {
		t.Fatalf("timeout not pushed live=%v parked=%v", live.AgentTimeout, parked.AgentTimeout)
	}
}

func TestBindSwarmLimitsIgnoresNil(t *testing.T) {
	e := newTestEngine(t)
	e.bindSwarmLimits(nil)
}

func TestApplyLiveSwarmLimitsRefreshesScheduleCaps(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	e.cfg.Swarm.ScheduleMaxActive = 0
	e.cfg.Swarm.ScheduleMinIntervalSeconds = 0
	e.ApplyLiveSwarmLimits()
	if e.maxActiveSchedules() != config.DefaultScheduleMaxActive {
		t.Fatalf("max=%d", e.maxActiveSchedules())
	}
	if e.scheduleMinInterval() != time.Duration(config.DefaultScheduleMinIntervalSeconds)*time.Second {
		t.Fatalf("min=%s", e.scheduleMinInterval())
	}
	e.cfg.Swarm.ScheduleMaxActive = 4
	e.cfg.Swarm.ScheduleMinIntervalSeconds = 45
	wantDefault := e.cfg.Models.Default
	e.cfg.Models.Default = "nope"
	e.ApplyLiveSwarmLimits()
	if e.maxActiveSchedules() != 4 {
		t.Fatalf("max=%d, settings must refresh the ticker snapshot", e.maxActiveSchedules())
	}
	if e.scheduleMinInterval() != 45*time.Second {
		t.Fatalf("min=%s", e.scheduleMinInterval())
	}
	if e.scheduleDefaultProvider() != "nope" {
		t.Fatalf("default=%q", e.scheduleDefaultProvider())
	}
	e.cfg.Models.Default = wantDefault
	e.ApplyLiveSwarmLimits()
	if e.scheduleDefaultProvider() != wantDefault {
		t.Fatalf("default=%q", e.scheduleDefaultProvider())
	}
}

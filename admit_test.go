package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
)

func holdRegistry(t *testing.T, cap int) (*Registry, chan struct{}) {
	t.Helper()
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.MaxConcurrent = cap
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &holdModel{started: make(chan struct{}), release: block}
	}
	return reg, block
}

func mustAgentID(t *testing.T, out string) string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("agent id: %v (%s)", err, out)
	}
	if m["agent_id"] == "" {
		t.Fatalf("no agent_id in %s", out)
	}
	return m["agent_id"]
}

func waitRosterID(t *testing.T, reg *Registry, id string) {
	t.Helper()
	h, ok := reg.get(id)
	if !ok {
		t.Fatalf("missing %s", id)
	}
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", id)
	}
}

func jsonHasError(out string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return false
	}
	_, ok := m["error"]
	return ok
}

func TestSpawnToolNamesTheLiveConcurrencyCap(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.MaxConcurrent = 5
	spawnInfo, err := reg.Tools()[0].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resumeInfo, err := reg.Tools()[4].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, desc := range []string{spawnInfo.Desc, resumeInfo.Desc} {
		if !strings.Contains(desc, "The concurrency cap is 5.") {
			t.Fatalf("tool hid the cap: %s", desc)
		}
	}
}

func TestSpawnToolRefusesPastTheConcurrencyCap(t *testing.T) {
	// A batch larger than the cap used to mint every worker and park the
	// extras on the gate. They showed up as running and burned the watchdog
	// before they ever called the model. The tool must refuse instead.
	reg, _ := holdRegistry(t, 1)
	spawnT := invokable(t, reg.Tools()[0])
	if _, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"do the assigned work"}`); err != nil {
		t.Fatal(err)
	}
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"beta","task":"do the assigned work"}`)
	assertCtlRefuse(t, out, err, "concurrency cap is 1")
	running, _ := reg.Stats()
	if running != 1 {
		t.Fatalf("refused spawn still occupied a slot: running=%d", running)
	}
	// Programmatic Spawn still queues. Settings can raise the cap and wake it.
	if _, err := reg.Spawn(context.Background(), "gamma", "do the assigned work", reg.ModelBuilder); err != nil {
		t.Fatalf("library Spawn should still queue: %v", err)
	}
	running, _ = reg.Stats()
	if running != 2 {
		t.Fatalf("queued library worker missing: running=%d", running)
	}
}

func TestParallelSpawnsAdmitOnlyTheCap(t *testing.T) {
	reg, _ := holdRegistry(t, 2)
	spawnT := invokable(t, reg.Tools()[0])
	const n = 6
	outs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			role := "w" + string(rune('a'+i))
			outs[i], errs[i] = spawnT.InvokableRun(context.Background(),
				`{"role":"`+role+`","task":"do the assigned work"}`)
		}(i)
	}
	wg.Wait()
	admitted := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("spawn %d returned a Go error: %v", i, errs[i])
		}
		if !jsonHasError(outs[i]) {
			admitted++
		}
	}
	if admitted != 2 {
		t.Fatalf("admitted %d, want the cap 2; outs=%v", admitted, outs)
	}
	running, _ := reg.Stats()
	if running != 2 {
		t.Fatalf("roster has %d live workers", running)
	}
}

func TestSameRoleSteerDoesNotNeedAFreeSlot(t *testing.T) {
	reg, _ := holdRegistry(t, 1)
	spawnT := invokable(t, reg.Tools()[0])
	first, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"do the assigned work"}`)
	if err != nil || jsonHasError(first) {
		t.Fatalf("first spawn: %v %s", err, first)
	}
	again, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"the next piece"}`)
	if err != nil || jsonHasError(again) {
		t.Fatalf("steering the running role must not take a new slot: %v %s", err, again)
	}
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"beta","task":"do the assigned work"}`)
	assertCtlRefuse(t, out, err, "concurrency cap is 1")
}

func TestResumeToolWaitsForAFreeSlot(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.MaxConcurrent = 1
	betaStarted := make(chan struct{})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == "beta" {
			return &holdModel{started: betaStarted, release: block}
		}
		return oneShot("done")(role, agentID)
	}
	spawnT := invokable(t, reg.Tools()[0])
	resumeT := invokable(t, reg.Tools()[4])
	first, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"do the assigned work"}`)
	if err != nil || jsonHasError(first) {
		t.Fatalf("alpha: %v %s", err, first)
	}
	alphaID := mustAgentID(t, first)
	waitRosterID(t, reg, alphaID)
	second, err := spawnT.InvokableRun(context.Background(), `{"role":"beta","task":"do the assigned work"}`)
	if err != nil || jsonHasError(second) {
		t.Fatalf("beta: %v %s", err, second)
	}
	betaID := mustAgentID(t, second)
	select {
	case <-betaStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("beta never started")
	}
	out, err := resumeT.InvokableRun(context.Background(), `{"agent_id":"`+alphaID+`","task":"the next piece"}`)
	assertCtlRefuse(t, out, err, "concurrency cap is 1")
	close(block)
	waitRosterID(t, reg, betaID)
	out, err = resumeT.InvokableRun(context.Background(), `{"agent_id":"`+alphaID+`","task":"the next piece"}`)
	if err != nil || jsonHasError(out) {
		t.Fatalf("resume after a slot freed: %v %s", err, out)
	}
}

func TestForkedSpawnRefusesPastTheCap(t *testing.T) {
	reg, _ := holdRegistry(t, 1)
	spawnT := invokable(t, reg.Tools()[0])
	if _, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"do the assigned work"}`); err != nil {
		t.Fatal(err)
	}
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"beta","task":"do the assigned work","fork_context":true}`)
	assertCtlRefuse(t, out, err, "concurrency cap is 1")
}

func TestSameRoleResumeRefusesWhileAnotherWorkerHoldsTheCap(t *testing.T) {
	// spawn_agent on a finished role resumes that id. That still takes a
	// slot, so a full cap must refuse it the same way resume_agent does.
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.MaxConcurrent = 1
	betaStarted := make(chan struct{})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == "beta" {
			return &holdModel{started: betaStarted, release: block}
		}
		return oneShot("done")(role, agentID)
	}
	spawnT := invokable(t, reg.Tools()[0])
	first, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"do the assigned work"}`)
	if err != nil || jsonHasError(first) {
		t.Fatalf("alpha: %v %s", err, first)
	}
	waitRosterID(t, reg, mustAgentID(t, first))
	second, err := spawnT.InvokableRun(context.Background(), `{"role":"beta","task":"do the assigned work"}`)
	if err != nil || jsonHasError(second) {
		t.Fatalf("beta: %v %s", err, second)
	}
	select {
	case <-betaStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("beta never started")
	}
	out, err := spawnT.InvokableRun(context.Background(), `{"role":"alpha","task":"the next piece"}`)
	assertCtlRefuse(t, out, err, "concurrency cap is 1")
}

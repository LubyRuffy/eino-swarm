package engine

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Run the real lock cycle in a child: a regression must leave its timeout stack,
// not strand engine actors and cleanup in the parent test process.
func TestFreshRegistryRestoreDoesNotDeadlockAnInterruptedGeneration(t *testing.T) {
	const child = "ZWAI_TEST_RESTORE_GENERATION_LOCK_CHILD"
	if os.Getenv(child) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary,
			"-test.run=^TestFreshRegistryRestoreDoesNotDeadlockAnInterruptedGeneration$",
			"-test.timeout=6s", "-test.count=1", "-test.v", "-engine.store-prefetch=false")
		command.Env = append(os.Environ(), child+"=1", "TMPDIR="+t.TempDir())
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fresh claim and interruption did not both finish: %v\n%s", err, output)
		}
		return
	}

	e := newTestEngine(t)
	thread, err := e.CreateThread("Concurrent restoration", "", "")
	if err != nil {
		t.Fatal(err)
	}
	original := mustStoredTurn(t, e, thread.ID, store.Turn{})
	next := mustStoredTurn(t, e, thread.ID, store.Turn{})
	rt := e.runtimeFor(thread.ID)
	acc := newAccumulator(e, thread.ID, original.ID, time.Hour)
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "original-output"})
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "worker", MessageID: "original-output", Text: "An unfinished observation."})
	generationHeld, resumeGeneration := make(chan struct{}), make(chan struct{})
	acc.beforeLiveEmit = func() {
		close(generationHeld)
		<-resumeGeneration
	}
	interrupted := make(chan struct{})
	go func() {
		acc.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "original-output", Text: generationBody(swarm.GenerationInterruption{Reason: "model_error", Detail: "Recorded interruption", Recovery: "none"})})
		close(interrupted)
	}()
	<-generationHeld // The actual notification owns generations.Lock here.

	claimHeld := make(chan struct{})
	claimed := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idle := make(chan struct{})
	registry := swarm.NewRegistry()
	defer registry.Close()
	go func() {
		got, reused, err := rt.claimRegistry(ctx, next, cancel, idle, func() (*swarm.Registry, error) {
			close(claimHeld) // The actual fresh claim owns rt.mu here.
			if _, err := rt.prepareLeftoverWorkers(ctx, next); err != nil {
				return nil, err
			}
			return registry, nil
		}, nil)
		if err == nil && (got != registry || reused) {
			err = store.ErrHistoryRegistryInheritanceInvalid
		}
		claimed <- err
	}()
	<-claimHeld
	close(resumeGeneration)
	// The child's Go timeout records both real stacks if either lock order
	// regresses. Successful actors are joined before engine/store cleanup.
	if err := <-claimed; err != nil {
		t.Fatal(err)
	}
	<-interrupted
	rt.release(cancel, idle, next.ID)
	events, err := e.store.ListGenerationEvents(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "generation_interrupted" && event.MessageID == "original-output" {
			found = event.TurnID == original.ID
		}
	}
	if !found {
		t.Fatalf("restoration changed the original generation receipt: %+v", events)
	}
}

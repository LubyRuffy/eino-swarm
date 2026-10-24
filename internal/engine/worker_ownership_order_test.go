package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"gorm.io/gorm"
)

// cleanup describes the old registry globally. Its durable sequence must
// precede a replacement registry becoming available, or replay can mark a
// newly started worker stopped even though only the old registry was killed.
func TestParkedWorkerCleanupPrecedesTheNextRegistryClaim(t *testing.T) {
	for _, action := range []string{"stop", "cancel-last-wake", "finalization-reap"} {
		t.Run(action, func(t *testing.T) {
			e := newTestEngine(t)
			// This ordering fixture installs GORM callbacks; unrelated schedule naming must not write concurrently.
			e.Config().Swarm.AutoTitle = false
			th, err := e.CreateThread("worker stop ordering", "", "")
			if err != nil {
				t.Fatal(err)
			}
			wake := mustCreateWake(t, e, th.ID)
			previous := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
			next := mustStoredTurn(t, e, th.ID, store.Turn{})
			reg, old := ownedBlockingRegistry(t)
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			rt.parked, rt.turnID = reg, previous.ID
			rt.mu.Unlock()
			if action == "finalization-reap" {
				if _, err := e.store.CancelSchedule(wake.ID); err != nil {
					t.Fatal(err)
				}
			}

			publishing, allowSave := make(chan struct{}), make(chan struct{})
			var allowOnce sync.Once
			release := func() { allowOnce.Do(func() { close(allowSave) }) }
			const callback = "worker-cleanup-before-next-registry"
			if err := e.store.DB().Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindCleanup {
					close(publishing)
					<-allowSave
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = e.store.DB().Callback().Create().Remove(callback) })
			t.Cleanup(release)
			stopped := make(chan error, 1)
			go func() {
				switch action {
				case "stop":
					stopped <- e.Interrupt(th.ID)
				case "cancel-last-wake":
					stopped <- e.CancelSchedule(wake.ID)
				case "finalization-reap":
					rt.reapUnownedParked()
					stopped <- nil
				}
			}()
			select {
			case <-publishing:
			case <-time.After(5 * time.Second):
				t.Fatal("old registry never reached its durable cleanup boundary")
			}

			nextReg := swarm.NewRegistry()
			t.Cleanup(nextReg.Close)
			entered := make(chan struct{})
			nextReg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idle := make(chan struct{})
			t.Cleanup(func() { rt.release(cancel, idle, next.ID) })
			type claim struct {
				taken    *swarm.Registry
				occupied bool
				worker   *swarm.Handle
				err      error
			}
			claimed := make(chan claim, 1)
			go func() {
				out := claim{taken: rt.takeParkedRegistry()}
				out.occupied = rt.occupy(nextReg, cancel, next.ID, idle)
				if out.occupied {
					out.worker, out.err = nextReg.Spawn(ctx, "next-inspector", "inspect the next assigned work", nextReg.ModelBuilder)
				}
				claimed <- out
			}()
			var out claim
			premature := false
			select {
			case out = <-claimed:
				premature = true
			case <-time.After(100 * time.Millisecond):
			}
			release()
			if !premature {
				select {
				case out = <-claimed:
				case <-time.After(5 * time.Second):
					t.Fatal("next registry stayed blocked after cleanup was saved")
				}
			}
			if err := <-stopped; err != nil {
				t.Fatal(err)
			}
			if premature {
				t.Fatal("the next registry and worker started before the old global cleanup could be persisted")
			}
			if out.taken != nil || !out.occupied || out.err != nil || out.worker == nil {
				t.Fatalf("replacement claim=%+v", out)
			}
			if !hasKind(t, e, th.ID, KindCleanup) {
				t.Fatal("next registry started without a durable old-worker cleanup receipt")
			}
			select {
			case <-old.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("old worker did not stop")
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("new worker did not enter its model")
			}
			select {
			case <-out.worker.Done():
				t.Fatal("old registry cleanup cancelled the newly claimed worker")
			default:
			}
		})
	}
}

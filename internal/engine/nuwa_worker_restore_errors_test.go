package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Each refusal crosses the real store/config/tool builder. No stub resolver
// may turn a broken original owner into the latest conversation's settings.
func TestNuwaTaggedConfigurationStorageAndBuilderErrorsFailClosed(t *testing.T) {
	for _, mode := range []string{"thread-read", "project-read", "tools", "provider", "cancel-after-turn-read"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngineAt(t, t.TempDir())
			project, thread, turn := nuwaValidationSource(t, e, "Tagged recovery failure")
			if err := e.store.DB().Model(turn).Update("project_id", project.ID).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ""
			switch mode {
			case "thread-read", "project-read":
				table := "threads"
				if mode == "project-read" {
					table = "projects"
				}
				want = mode + " fixture unavailable"
				failure := errors.New(want)
				fired := false
				if err := e.store.DB().Callback().Query().Before("gorm:query").Register("nuwa:test:owner-read-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == table && !fired {
						fired = true
						_ = tx.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "tools":
				want = "web_execution"
				if err := e.store.DB().Model(turn).Update("web_execution", "invalid-execution").Error; err != nil {
					t.Fatal(err)
				}
			case "provider":
				want = "unknown provider"
				if err := e.store.DB().Model(turn).Update("provider_id", "missing-provider").Error; err != nil {
					t.Fatal(err)
				}
			case "cancel-after-turn-read":
				if err := e.store.DB().Callback().Query().After("gorm:query").Register("nuwa:test:cancel-between-owner-reads", func(tx *gorm.DB) {
					if tx.Statement.Table == "turns" {
						cancel()
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			config, err := e.workerConfigurationForTag(ctx, thread.ID, turn.ID)
			if err == nil || config.ModelBuilder != nil {
				t.Fatalf("failed original configuration was accepted: %+v err=%v", config, err)
			}
			if mode == "cancel-after-turn-read" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost during owner probes: %v", err)
				}
			} else if !strings.Contains(strings.ToLower(err.Error()), want) {
				t.Fatalf("wrong failure: %v want %q", err, want)
			}
			var calls int64
			if err := e.store.DB().Model(&store.LLMCall{}).Where("thread_id = ?", thread.ID).Count(&calls).Error; err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatal("configuration error admitted a model request")
			}
		})
	}
}

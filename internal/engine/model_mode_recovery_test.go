package engine

import (
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The durable turn is the recovery authority. Its original gateway remains
// authorized even though the composer now offers only the third-party preset.
func TestRestartAfterModeSwitchKeepsTurnModelUntilNextTurn(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	srv := httptest.NewServer(recordCompletion(&mu, &requested))
	defer srv.Close()
	e := newAccountEngine(t, srv.URL+"/v1", "original-model")
	if err := e.UpdateConfig(func(c *config.Config) error { c.Product.Mode = config.ProductModeLocal; return nil }); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("Mode recovery", accountCatalogID, "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the interrupted task")
	mode := config.ProductConfig{Mode: config.ProductModeFOFA}
	if err := e.UpdateConfigForMode(&mode, func(c *config.Config) error { c.Product = mode; return nil }); err != nil {
		t.Fatal(err)
	}
	// A fresh pool represents the replacement process, with freshly authorized
	// catalogs rather than a cached model from the previous engine.
	e.pool = provider.New(e.cfg)
	original := accountCatalog(t, srv.URL+"/v1", "original-model")
	preset, err := provider.NewAccountSource("gateway:abcdefabcdefabcdefabcdefabcdefab", "Partner", srv.URL+"/v1", "token", provider.Catalog{Names: []string{"preset-model"}})
	if err != nil {
		t.Fatal(err)
	}
	preset.SetModelPolicy(false, map[string]string{"conversation": "preset-model"})
	e.pool.SetAccountSource(preset)
	e.pool.SetLocalAccountSources([]*provider.AccountSource{original})
	if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume count=%d err=%v", n, err)
	}
	if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone {
		t.Fatalf("resumed turn failed: %s", finished.Error)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	managers := 0
	for _, call := range calls {
		if call.AgentID == "manager" {
			managers++
			if call.ProviderID != accountCatalogID || call.Model != "original-model" {
				t.Fatalf("recovery retargeted the original manager: %+v", call)
			}
		}
	}
	if managers == 0 {
		t.Fatal("recovered manager made no recorded HTTP request")
	}
	parked, err := e.parkedRegistry(th, turn)
	if err != nil {
		t.Fatal(err)
	}
	defer parked.Close()
	if _, err := parked.ModelBuilder("worker", "parked-model-check").Generate(t.Context(), []*schema.Message{schema.UserMessage("continue")}); err != nil {
		t.Fatal(err)
	}
	configuration, err := e.workerConfigurationForTag(t.Context(), th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuration.ModelBuilder("worker", "tagged-model-check").Generate(t.Context(), []*schema.Message{schema.UserMessage("continue")}); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, e, turn.ID)
	failed := failedHumanTurn(t, e, th.ID, func(row *store.Turn) {
		row.ProviderID, row.Model = accountCatalogID, "original-model"
	})
	if err := e.store.AppendMessages(th.ID, failed.ID, []store.Message{{Role: "user", Content: failed.UserText}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RetryFailedTurn(th.ID, failed.ID); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, e, failed.ID)
	retried, err := e.store.ListLLMCalls(failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	retryManagers := 0
	for _, call := range retried {
		if call.AgentID == "manager" {
			retryManagers++
			if call.ProviderID != accountCatalogID || call.Model != "original-model" {
				t.Fatalf("same-turn Retry applied the next-turn preset: %+v", call)
			}
		}
	}
	if retryManagers == 0 {
		t.Fatal("Retry did not send a manager request")
	}
	beforeNext := len(snapshotModels(&mu, &requested))
	next, err := e.StartTurn(th.ID, "start the next request")
	if err != nil {
		t.Fatal(err)
	}
	if next.ProviderID != preset.ID() || next.Model != "preset-model" {
		t.Fatalf("next turn did not apply the new preset: %+v", next)
	}
	waitTurn(t, e, next.ID)
	if calls := snapshotModels(&mu, &requested); !onlyModels(calls[beforeNext:], "preset-model") {
		t.Fatalf("new turn reused the old source: %v", calls[beforeNext:])
	}
}

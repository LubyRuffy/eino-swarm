package remote

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestPhoneDoesNotPinTheConfiguredAccountDefault(t *testing.T) {
	e := accountPhoneEngine(t, "first-model", "saved-model")
	if err := e.Config().Update(func(c *config.Config) error {
		c.Models.AccountModel = "saved-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("", accountModelsSourceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if th.Model != "" {
		t.Fatalf("new conversation stored the settings default: %q", th.Model)
	}
	if err := applySelection(e, th.ID, Request{ProviderID: accountModelsSourceID, Model: "saved-model"}); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil || got.Model != "" {
		t.Fatalf("sending the settings default pinned %q (%v)", got.Model, err)
	}
	if err := applySelection(e, th.ID, Request{ProviderID: accountModelsSourceID, Model: "first-model"}); err != nil {
		t.Fatal(err)
	}
	got, err = e.Store().GetThread(th.ID)
	if err != nil || got.Model != "first-model" {
		t.Fatalf("an explicit other model was not stored: %q %v", got.Model, err)
	}
}

func TestPhoneCatalogShowsAMissingAccountDefaultAsUnavailable(t *testing.T) {
	e := accountPhoneEngine(t, "first-model", "second-model")
	if err := e.Config().Update(func(c *config.Config) error {
		c.Models.AccountModel = "removed-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var missing *ModelView
	for _, row := range modelViews(e) {
		if row.Model == "removed-model" {
			copy := row
			missing = &copy
		}
		if row.Model == "first-model" && row.Default {
			t.Fatalf("catalog order replaced the missing default: %+v", row)
		}
	}
	if missing == nil || missing.Ready || !missing.Default || !missing.DefaultFollowsSetting {
		t.Fatalf("missing default row: %+v", missing)
	}
}

func TestPhonePinsTheShownCatalogFallback(t *testing.T) {
	e := accountPhoneEngine(t, "first-model", "shown-model")
	th, err := e.CreateThread("", accountModelsSourceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"model": ""}); err != nil {
		t.Fatal(err)
	}
	if err := applySelection(e, th.ID, Request{ProviderID: accountModelsSourceID, Model: "shown-model"}); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil || got.Model != "shown-model" {
		t.Fatalf("shown catalog model was not remembered: %q %v", got.Model, err)
	}
}

func accountPhoneEngine(t *testing.T, names ...string) *engine.Engine {
	t.Helper()
	e := catalogEngine(t, config.Provider{
		ID: "endpoint", Label: "Endpoint", BaseURL: "https://own.invalid/v1", Model: "own-model",
	})
	if err := e.Config().Update(func(c *config.Config) error {
		c.Models.DefaultSource = config.DefaultSourceAccount
		c.Models.AccountModel = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	src, err := provider.NewAccountSource(accountModelsSourceID, "Server", "https://account.invalid/v1", "token", provider.Catalog{Names: names})
	if err != nil {
		t.Fatal(err)
	}
	e.Providers().SetAccountSource(src)
	return e
}

package server

import (
	"net/http"
	"reflect"
	"slices"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/gin-gonic/gin"
)

func (s *Server) currentSettingsView() settingsView {
	cfg := s.engine.Config().Snapshot()
	v := toSettingsView(cfg)
	policy := s.engine.ModelPolicyView()
	v.ModelPolicy = policy
	if policy.Configurable {
		v.Models.Bindings = cfg.Models.Bindings[policy.Scope]
		return v
	}
	v.Models.Providers = []providerView{}
	v.Models.Default, v.Models.AccountModel, v.Models.AccountPresentation = "", "", ""
	v.Models.DefaultSource = config.DefaultSourceAccount
	v.Swarm.TitleProvider, v.Swarm.TitleModel = "", ""
	v.Swarm.CompactProvider, v.Swarm.CompactModel = "", ""
	v.Search.EmbeddingProvider, v.Search.EmbeddingModel = "", ""
	v.Speech.Input.SpeechEndpoint = config.SpeechEndpoint{Enabled: cfg.Speech.Input.Enabled, Source: config.SpeechSourceAccount}
	v.Speech.Output.SpeechEndpoint = config.SpeechEndpoint{Enabled: cfg.Speech.Output.Enabled, Source: config.SpeechSourceAccount}
	v.Speech.Input.HasAPIKey, v.Speech.Output.HasAPIKey = false, false
	return v
}

// A sanitized hidden row can round-trip during an unrelated autosave. Preserve
// its private selection; a new concrete model must never change a managed preset.
func validatePresetSettings(old, next *config.Config, policy provider.ModelPolicy) error {
	scope := policy.Scope
	if next.Product.Resolved() == config.ProductModeLocal {
		scope = config.ProductModeLocal
	}
	image := next.Models.Bindings[scope]["image"]
	if image != (config.ModelRef{}) && image != old.Models.Bindings[scope]["image"] && !imageBindingAllowed(policy, image) {
		return errCustomModelsDisabled
	}
	if next.Product.Resolved() != config.ProductModeLocal && policy.CapabilityStatus != "" {
		previous, incoming := old.Models.Bindings[policy.Scope], next.Models.Bindings[policy.Scope]
		for _, role := range append(config.ModelRoles(), config.ComposerPredictionRole) {
			if reflect.DeepEqual(previous[role], incoming[role]) {
				continue
			}
			if role == "image" && policy.Configurable && image == (config.ModelRef{}) {
				continue
			}
			capability := role
			if role == config.ComposerPredictionRole {
				capability = "conversation"
			}
			if policy.Capabilities == nil || !policy.Capabilities.Allows(capability) || !policy.Capabilities.Capabilities[capability].Configurable {
				return errCustomModelsDisabled
			}
		}
	}
	if next.Product.Resolved() == config.ProductModeLocal || policy.Configurable {
		return nil
	}
	pins := [][2]*string{{&next.Swarm.TitleProvider, &old.Swarm.TitleProvider}, {&next.Swarm.TitleModel, &old.Swarm.TitleModel}, {&next.Swarm.CompactProvider, &old.Swarm.CompactProvider}, {&next.Swarm.CompactModel, &old.Swarm.CompactModel}, {&next.Search.EmbeddingProvider, &old.Search.EmbeddingProvider}, {&next.Search.EmbeddingModel, &old.Search.EmbeddingModel}}
	for _, pin := range pins {
		if *pin[0] != "" && *pin[0] != *pin[1] {
			return errCustomModelsDisabled
		}
		*pin[0] = *pin[1]
	}
	for _, pair := range [][2]*config.SpeechEndpoint{{&next.Speech.Input.SpeechEndpoint, &old.Speech.Input.SpeechEndpoint}, {&next.Speech.Output.SpeechEndpoint, &old.Speech.Output.SpeechEndpoint}} {
		incoming, previous := *pair[0], *pair[1]
		enabled := incoming.Enabled
		// Enablement belongs to the user's speech preference, not model selection.
		incoming.Enabled, previous.Enabled = false, false
		// mergeSpeechSettings retains an omitted write-only key. A sanitized
		// account placeholder may therefore contain the unchanged private key.
		empty := incoming == (config.SpeechEndpoint{Source: config.SpeechSourceAccount}) ||
			incoming == (config.SpeechEndpoint{Source: config.SpeechSourceAccount, APIKey: previous.APIKey})
		if !empty && incoming != previous {
			return errCustomModelsDisabled
		}
		*pair[0] = *pair[1]
		pair[0].Enabled = enabled
	}
	if !reflect.DeepEqual(old.Models.Bindings, next.Models.Bindings) {
		return errCustomModelsDisabled
	}
	return nil
}

func imageBindingAllowed(policy provider.ModelPolicy, ref config.ModelRef) bool {
	source := policy.AccountSources[ref.ProviderID]
	if !source.Allows("image") || !source.Configurable || !source.Capabilities["image"].Configurable {
		return false
	}
	for _, catalog := range policy.FunctionalCatalogs {
		if catalog.ProviderID == ref.ProviderID {
			return slices.Contains(catalog.ImageModels, ref.Model)
		}
	}
	return false
}

func (s *Server) refreshCapabilities(c *gin.Context) {
	if err := s.engine.Providers().RefreshCapabilities(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "capability_discovery_unavailable"})
		return
	}
	c.JSON(http.StatusOK, s.engine.ModelPolicyView())
}

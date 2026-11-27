package config

import (
	"errors"
	"maps"
	"strings"
)

// ModelRef is a credential-free functional model selection.
type ModelRef struct {
	ProviderID string `yaml:"provider_id" json:"provider_id"`
	Model      string `yaml:"model" json:"model"`
}

// ComposerPredictionRole is a client binding that uses the conversation capability.
const ComposerPredictionRole = "composer_prediction"

// ModelRoles is the existing functional model surface, in settings order.
func ModelRoles() []string {
	return []string{"conversation", "title", "compact", "translation", "embedding", "ocr", "decisions", "transcription", "speech", "image"}
}

// KnownModelRole validates a functional binding without accepting arbitrary keys.
func KnownModelRole(role string) bool {
	for _, known := range ModelRoles() {
		if known == role {
			return true
		}
	}
	return false
}

func cloneBindings(bindings map[string]map[string]ModelRef) map[string]map[string]ModelRef {
	if bindings == nil {
		return nil
	}
	out := make(map[string]map[string]ModelRef, len(bindings))
	for scope, refs := range bindings {
		out[scope] = maps.Clone(refs)
	}
	return out
}

func (c *Config) validateModelBindings() error {
	for scope, refs := range c.Models.Bindings {
		if scope != ProductModeLocal && !strings.HasPrefix(scope, "gateway:") {
			return errors.New("config: invalid model binding scope")
		}
		for role, ref := range refs {
			// An empty embedding reference explicitly disables semantic search,
			// including a gateway default and any legacy search pin.
			if role == "embedding" && ref == (ModelRef{}) {
				continue
			}
			if (!KnownModelRole(role) && role != ComposerPredictionRole) || strings.TrimSpace(ref.ProviderID) == "" || strings.TrimSpace(ref.Model) == "" {
				return errors.New("config: a model binding needs a known role, provider_id and model")
			}
			// A gateway: id in the local scope names a signed-in catalog. It is
			// not written into models.providers. Sign-out fails that binding
			// closed instead of substituting a personal provider.
		}
	}
	return nil
}

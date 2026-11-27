package engine

import "github.com/LubyRuffy/eino-swarm/internal/provider"

// roleModel picks the fixed role for a FOFA account provider. ok is false when
// this install or provider is not in role mode, and the caller keeps its own
// model. An enabled role returns its id. Title and compact stay on when their
// switch is off, using conversation. Other roles return an empty model so the
// caller does not make the call.
func (e *Engine) roleModel(providerID, role string) (modelName string, ok bool) {
	if e == nil || e.cfg == nil || e.pool == nil {
		return "", false
	}
	ref, configured := e.pool.RoleSelection(role, providerID, "")
	return ref.Model, configured
}

func (e *Engine) applyRoleModel(providerID, role, fallbackProvider, fallbackModel string) (string, string) {
	ref, _ := e.pool.RoleSelection(role, fallbackProvider, fallbackModel)
	return ref.ProviderID, ref.Model
}

// enabledAccountRole returns a FOFA role only while that role is switched on.
// A missing or disabled role leaves the caller on its existing model.
func (e *Engine) enabledAccountRole(role string) (providerID, modelName string, ok bool) {
	if e == nil || e.cfg == nil || e.pool == nil || !e.cfg.Product.FOFA() {
		return "", "", false
	}
	id := e.pool.AccountSourceID()
	enabled, configured := e.pool.AccountRole(id, role)
	if !configured || !enabled {
		return "", "", false
	}
	return id, role, true
}

// ModelPolicyView adds client preferences to the provider's permission view.
func (e *Engine) ModelPolicyView() provider.ModelPolicy {
	policy := e.pool.ModelPolicy()
	translation := e.cfg.SwarmSnapshot().SelectionTranslation
	policy.SelectionTranslation = &translation
	return policy
}

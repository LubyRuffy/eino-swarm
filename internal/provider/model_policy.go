package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"net/url"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// ErrModelPolicy refuses a model outside the current execution source.
var ErrModelPolicy = errors.New("model_selection_disabled: model is outside the current preset")

// ModelPolicy is the safe, account-scoped projection consumed by PC and phone.
type ModelPolicy struct {
	AccountSources       map[string]*SourceCapabilities `json:"account_sources,omitempty"`
	Capabilities         *SourceCapabilities            `json:"capabilities,omitempty"`
	CapabilityStatus     string                         `json:"capability_status,omitempty"`
	SelectionTranslation *bool                          `json:"selection_translation,omitempty"`
	Mode                 string                         `json:"mode"`
	Configurable         bool                           `json:"configurable"`
	Ready                bool                           `json:"ready"`
	Scope                string                         `json:"scope"`
	Defaults             map[string]config.ModelRef     `json:"defaults,omitempty"`
	Bindings             map[string]config.ModelRef     `json:"bindings,omitempty"`
	Roles                []AccountRole                  `json:"roles,omitempty"`
	FunctionalCatalogs   []FunctionalCatalog            `json:"functional_catalogs,omitempty"`
}

// FunctionalCatalog is one signed-in catalog's non-conversation names.
// The composer list omits them. Functional model settings read each typed
// list from the catalog that published it.
type FunctionalCatalog struct {
	ProviderID          string   `json:"provider_id"`
	Label               string   `json:"label,omitempty"`
	CatalogGroup        string   `json:"catalog_group,omitempty"`
	EmbeddingModels     []string `json:"embedding_models,omitempty"`
	TranscriptionModels []string `json:"transcription_models,omitempty"`
	SpeechModels        []string `json:"speech_models,omitempty"`
	ImageModels         []string `json:"image_models,omitempty"`
}

// SetModelPolicy installs the server's edit permission and role defaults.
// Defaults name only models on this source; credentials remain private.
func (s *AccountSource) SetModelPolicy(configurable bool, defaults map[string]string) {
	s.managed = !configurable
	s.defaults = maps.Clone(defaults)
}

// SetAccountOwner separates saved overrides when the same gateway source id is
// reused for several accounts. Token renewal does not change this identity.
func (s *AccountSource) SetAccountOwner(owner string) { s.owner = owner }

func (p *Pool) activeAccount() *AccountSource {
	if p.localExecution() {
		return nil
	}
	return p.account.Load()
}

func (p *Pool) localExecution() bool {
	return p != nil && p.cfg != nil && p.cfg.ProductSnapshot().Resolved() == config.ProductModeLocal
}

// ModelPolicy separates debug source selection from server-owned model policy.
func (p *Pool) ModelPolicy() ModelPolicy {
	if p.cfg.ProductSnapshot().Resolved() == config.ProductModeLocal {
		sources := map[string]*SourceCapabilities{}
		for _, source := range p.effectiveLocalAccountSources() {
			sources[source.id] = cloneCapabilities(source.capabilities)
		}
		status := ""
		if primary := p.account.Load(); primary != nil && primary.negotiated {
			status = "unavailable"
			if primary.capabilities != nil {
				status = "ready"
			}
		}
		return ModelPolicy{CapabilityStatus: status, AccountSources: sources, Mode: "local", Configurable: true, Ready: true, Scope: config.ProductModeLocal, Bindings: p.cfg.ModelsSnapshot().Bindings[config.ProductModeLocal], FunctionalCatalogs: p.functionalCatalogs()}
	}
	view := ModelPolicy{Mode: "gateway"}
	src := p.account.Load()
	if src == nil {
		return view
	}
	view.Scope = src.bindingScope()
	if view.Scope == "" {
		return view
	}
	view.Configurable, view.Ready = !src.managed, src.id != ""
	view.Capabilities = cloneCapabilities(src.capabilities)
	view.AccountSources = map[string]*SourceCapabilities{src.id: view.Capabilities}
	if src.negotiated {
		view.CapabilityStatus = "unavailable"
		view.Configurable = view.Configurable && src.capabilities != nil && src.capabilities.Authorized && src.capabilities.Configurable
		view.Ready = view.Ready && src.allowsRole("conversation")
		if src.capabilities != nil {
			view.CapabilityStatus = "ready"
		}
	}
	if view.Configurable {
		view.Bindings = p.cfg.ModelsSnapshot().Bindings[view.Scope]
	}
	view.Roles = src.Roles()
	view.Defaults = make(map[string]config.ModelRef, len(src.defaults))
	for role, name := range src.defaults {
		view.Defaults[role] = config.ModelRef{ProviderID: src.id, Model: name}
	}
	view.FunctionalCatalogs = p.functionalCatalogs()
	return view
}

func (s *AccountSource) bindingScope() string {
	origin, err := url.Parse(s.baseURL)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(origin.Scheme + "://" + origin.Host + "\x00" + s.id + "\x00" + s.owner))
	return "gateway:" + hex.EncodeToString(sum[:])
}

// functionalCatalogs publishes typed names that the conversation list removes.
// A switchable catalog owns its id when the signed-in projection reuses it.
func (p *Pool) functionalCatalogs() []FunctionalCatalog {
	seen := map[string]bool{}
	var out []FunctionalCatalog
	add := func(src *AccountSource) {
		if src == nil || src.unavailable || src.id == "" || seen[src.id] {
			return
		}
		seen[src.id] = true
		kinds := src.Kinds()
		if len(kinds.EmbeddingModels) == 0 && len(kinds.TranscriptionModels) == 0 && len(kinds.SpeechModels) == 0 && len(kinds.ImageModels) == 0 {
			return
		}
		out = append(out, FunctionalCatalog{
			ProviderID: src.id, Label: strings.TrimSpace(src.label), CatalogGroup: src.catalogGroup,
			EmbeddingModels: kinds.EmbeddingModels, TranscriptionModels: kinds.TranscriptionModels, SpeechModels: kinds.SpeechModels, ImageModels: kinds.ImageModels,
		})
	}
	if p.localExecution() {
		for _, src := range p.localAccountSources() {
			add(src)
		}
		add(p.account.Load())
		return out
	}
	add(p.activeAccount())
	return out
}

// StoredConversationModel resolves the model a stored conversation runs.
// A managed preset keeps that binding for a later LOCAL session and executes
// its conversation model. Explicit choices still go through
// ValidateConversationSelection.
func (p *Pool) StoredConversationModel(id, model string) (config.Provider, error) {
	if p.OfficialOnly() {
		src := p.activeAccount()
		if src.unavailable {
			return config.Provider{}, ErrAccountModelsUnavailable
		}
		return src.provider(""), nil
	}
	return p.ResolveModel(id, model)
}

// ValidateConversationSelection checks explicit client choices. Historical
// conversation bindings are resolved separately so a managed login can retain
// them for a later LOCAL session without executing them.
func (p *Pool) ValidateConversationSelection(id, name string) error {
	if !p.FeatureAvailable(id, "conversation") && !p.mock {
		return ErrModelPolicy
	}
	if src := p.activeAccount(); src != nil && src.managed {
		if (id != "" && id != src.id) || (name != "" && name != src.provider("").Model) {
			return ErrModelPolicy
		}
	}
	_, err := p.ResolveModel(id, name)
	return err
}

// RoleSelection resolves one functional model. An empty result is unavailable.
// Managed selections never merge personal providers or legacy local pins.
func (p *Pool) RoleSelection(role, fallbackProvider, fallbackModel string) (config.ModelRef, bool) {
	policy := p.ModelPolicy()
	src := p.activeAccount()
	if src == nil && strings.HasPrefix(fallbackProvider, config.AccountSourcePrefix) {
		src = p.accountForExecution(fallbackProvider)
	}
	if src != nil && !src.allowsRole(role) {
		return config.ModelRef{}, true
	}
	if src != nil && (src.managed || src.negotiated && !src.capabilities.Capabilities[role].Configurable) {
		if on, configured := src.Role(role); configured {
			if on {
				if name := src.defaults[role]; name != "" {
					return config.ModelRef{ProviderID: src.id, Model: name}, true
				}
				return config.ModelRef{ProviderID: src.id, Model: role}, true
			}
			if role == "title" || role == "compact" || role == "translation" && !src.negotiated {
				return config.ModelRef{ProviderID: src.id, Model: src.provider("").Model}, true
			}
			return config.ModelRef{}, true
		}
		if name := src.defaults[role]; name != "" {
			return config.ModelRef{ProviderID: src.id, Model: name}, true
		}
		if role == "conversation" || role == "title" || role == "compact" {
			return config.ModelRef{ProviderID: src.id, Model: src.provider("").Model}, true
		}
		return config.ModelRef{}, true
	}
	if ref, ok := p.cfg.ModelsSnapshot().Bindings[policy.Scope][role]; ok {
		return ref, true
	}
	if src != nil {
		if name := src.defaults[role]; name != "" {
			return config.ModelRef{ProviderID: src.id, Model: name}, true
		}
		// An editable Gateway may publish typed audio models without naming a
		// default. Use the first authorized role match, as the phone does.
		if src.negotiated && !p.localExecution() && (role == "transcription" || role == "speech") {
			for _, name := range src.names {
				if src.types[name] == role {
					return config.ModelRef{ProviderID: src.id, Model: name}, true
				}
			}
		}
	}
	return config.ModelRef{ProviderID: fallbackProvider, Model: fallbackModel}, false
}

func (s *AccountSource) permits(name string) bool {
	for _, n := range s.names {
		if name == n {
			return true
		}
	}
	for _, n := range s.defaults {
		if name == n {
			return true
		}
	}
	for _, r := range s.roles {
		if r.ID == name && r.Enabled {
			return true
		}
	}
	return false
}

// RoleProvider resolves non-conversational requests without rewriting them to
// the conversational alias. Empty bindings remain explicitly unavailable.
func (p *Pool) RoleProvider(role string) (config.Provider, bool, error) {
	ref, configured := p.RoleSelection(role, "", "")
	if !configured {
		return config.Provider{}, false, nil
	}
	if ref.ProviderID == "" || ref.Model == "" {
		return config.Provider{}, true, ErrModelPolicy
	}
	prov, err := p.resolveRoleProvider(role, ref)
	return prov, true, err
}

func (p *Pool) resolveRoleProvider(role string, ref config.ModelRef) (config.Provider, error) {
	if err := p.validateModelSelection(ref.ProviderID, ref.Model); err != nil {
		return config.Provider{}, err
	}
	prov, err := p.Resolve(ref.ProviderID)
	prov.Model = ref.Model
	if src := p.accountForExecution(ref.ProviderID); src != nil && src.negotiated {
		if !src.allowsRole(role) {
			return config.Provider{}, ErrModelPolicy
		}
		prov.APIMode = src.chatAPI(role)
	}
	if src := p.activeAccount(); src != nil && src.id == ref.ProviderID {
		for _, r := range src.roles {
			if r.ID == role {
				prov.Label = r.Label
			}
		}
	}
	return prov, err
}

func (p *Pool) validateModelSelection(id, name string) error {
	if p.mock {
		return nil
	}
	if source := p.accountForExecution(id); source != nil && source.negotiated && (source.capabilities == nil || !source.capabilities.Authorized || !source.capabilities.Available) {
		return ErrAccountModelsUnavailable
	}
	if p.localExecution() && strings.HasPrefix(id, config.AccountSourcePrefix) {
		source := p.accountForExecution(id)
		if source == nil {
			return ErrAccountLoginRequired
		}
		if source.unavailable {
			return ErrAccountModelsUnavailable
		}
		if name != "" && !source.permits(name) {
			return ErrModelPolicy
		}
		return nil
	}
	src := p.activeAccount()
	if src != nil && src.unavailable && id == src.id {
		return ErrAccountModelsUnavailable
	}
	if strings.HasPrefix(id, config.AccountSourcePrefix) && (src == nil || src.id != id) {
		return ErrAccountLoginRequired
	}
	if src != nil && src.managed && (id != src.id || (name != "" && !src.permits(name))) {
		return ErrModelPolicy
	}
	return nil
}

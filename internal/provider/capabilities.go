package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
)

// RoleCapability contains public protocol identifiers, never model credentials.
type RoleCapability struct {
	AudioParametersVersion    int      `json:"audio_parameters_version,omitempty"`
	Supported                 bool     `json:"supported"`
	Available                 bool     `json:"available"`
	Configurable              bool     `json:"configurable"`
	Endpoints                 []string `json:"endpoints"`
	ResponsesModes            []string `json:"responses_modes,omitempty"`
	ResponsesDeveloperHistory int      `json:"responses_developer_history,omitempty"`
	Reason                    string   `json:"reason,omitempty"`
}

type SourceCapabilities struct {
	ResponsesRequestGzip bool                      `json:"responses_request_gzip,omitempty"`
	Authorized           bool                      `json:"authorized"`
	Available            bool                      `json:"available"`
	Configurable         bool                      `json:"configurable"`
	Capabilities         map[string]RoleCapability `json:"capabilities"`
}

type ClientCapabilities struct {
	Version           int                            `json:"version"`
	ModelRolesVersion int                            `json:"model_roles_version"`
	Sources           map[string]*SourceCapabilities `json:"sources"`
}

// ParseCapabilities refuses unknown protocols. Missing roles remain unavailable.
func ParseCapabilities(raw []byte) (*ClientCapabilities, error) {
	var envelope struct {
		Data ClientCapabilities `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Data.Version != 1 || envelope.Data.ModelRolesVersion != 3 {
		return nil, errors.New("provider: unsupported capability manifest")
	}
	if len(envelope.Data.Sources) != 2 || envelope.Data.Sources["preset"] == nil || envelope.Data.Sources["gateway"] == nil {
		return nil, errors.New("provider: missing capability sources")
	}
	for _, source := range envelope.Data.Sources {
		if source == nil || len(source.Capabilities) > 10 {
			return nil, errors.New("provider: invalid capability source")
		}
		for role, capability := range source.Capabilities {
			if !config.KnownModelRole(role) || len(capability.Endpoints) > 16 || len(capability.ResponsesModes) > 16 || len(capability.Reason) > 256 {
				return nil, errors.New("provider: invalid role capability")
			}
			for _, endpoint := range capability.Endpoints {
				if len(endpoint) > 64 {
					return nil, errors.New("provider: invalid capability endpoint")
				}
			}
		}
	}
	return &envelope.Data, nil
}

// ImplementedEndpoints describes the public protocol adapters in this client.
func ImplementedEndpoints(role string) []string {
	switch role {
	case "conversation", "title", "compact", "translation", "ocr":
		return []string{"chat_completions", "responses"}
	case "embedding":
		return []string{"embeddings"}
	case "decisions":
		return []string{"decisions"}
	case "transcription":
		return []string{"audio_transcriptions"}
	case "speech":
		return []string{"audio_speech"}
	case "image":
		return []string{"images"}
	default:
		return nil
	}
}

func (s *SourceCapabilities) Allows(role string) bool {
	if s == nil || !s.Authorized || !s.Available {
		return false
	}
	c := s.Capabilities[role]
	if !c.Supported || !c.Available {
		return false
	}
	for _, endpoint := range ImplementedEndpoints(role) {
		if slices.Contains(c.Endpoints, endpoint) {
			return true
		}
	}
	return false
}

func cloneCapabilities(source *SourceCapabilities) *SourceCapabilities {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Capabilities = maps.Clone(source.Capabilities)
	for role, capability := range copy.Capabilities {
		capability.Endpoints = slices.Clone(capability.Endpoints)
		capability.ResponsesModes = slices.Clone(capability.ResponsesModes)
		copy.Capabilities[role] = capability
	}
	return &copy
}

// SetCapabilities seals a remotely negotiated source, including unknown/failure.
// Sources created directly by library callers retain their legacy behavior.
func (s *AccountSource) SetCapabilities(value *SourceCapabilities) {
	s.negotiated = true
	s.discoveryInterrupted = false
	s.capabilities = cloneCapabilities(value)
}

func (s *AccountSource) allowsRole(role string) bool {
	return s != nil && (!s.negotiated || s.capabilities.Allows(role))
}

func (s *AccountSource) chatAPI(role string) string {
	if s.capabilities != nil && slices.Contains(s.capabilities.Capabilities[role].Endpoints, config.APIModeResponses) {
		return config.APIModeResponses
	}
	return config.APIModeChatCompletions
}

func (p *Pool) guardCapability(inner model.BaseChatModel, id, role string) model.BaseChatModel {
	if !strings.HasPrefix(id, config.AccountSourcePrefix) {
		return inner
	}
	if !config.KnownModelRole(role) {
		role = "conversation"
	}
	guarded := &recordingModel{inner: inner}
	if shared, ok := inner.(*recordingModel); ok {
		// Keep capability checks on the existing request admission. A second
		// recording layer loses the gate identity and counts cancelled waits
		// as calls before the inner layer has admitted any HTTP request.
		*guarded = *shared
	}
	policyUse := guarded.policyUse
	roleSource := guarded.roleSource
	if roleSource == nil {
		roleSource = func() *AccountSource { return p.accountForExecution(id) }
	}
	guarded.onResult = p.capabilityResult
	guarded.policyUse = func() (func(), error) {
		acquire := policyUse
		if acquire == nil {
			acquire = p.beginChatAdmission
		}
		release, err := acquire()
		if err != nil {
			return nil, err
		}
		source := roleSource()
		if !source.allowsRole(role) || source.negotiated && guarded.apiMode != "" &&
			!slices.Contains(source.capabilities.Capabilities[role].Endpoints, guarded.apiMode) {
			release()
			return nil, ErrModelPolicy
		}
		return release, nil
	}
	return guarded
}

type recoverChatDiscoveryKey struct{}

// WithChatDiscoveryRecovery allows background model preparation to wait for a
// fresh grant. Synchronous turn admission may build without waiting on discovery.
func WithChatDiscoveryRecovery(ctx context.Context) context.Context {
	return context.WithValue(ctx, recoverChatDiscoveryKey{}, true)
}

// Recover selection before building a client so the wire protocol comes from
// the fresh capability, even if the temporarily closed projection had none.
func (p *Pool) recoverChatSelection(ctx context.Context, prev config.Provider, bound *AccountSource, id, name string) (config.Provider, error) {
	refreshErr := p.RefreshCapabilities(ctx)
	if err := ctx.Err(); err != nil {
		return config.Provider{}, err
	}
	if refreshErr != nil {
		return config.Provider{}, errCapabilityDiscoveryInterrupted
	}
	current := p.accountForExecution(prev.ID)
	if current == nil {
		return config.Provider{}, ErrAccountLoginRequired
	}
	if current.discoveryInterrupted {
		return config.Provider{}, errCapabilityDiscoveryInterrupted
	}
	if !p.credentialMatches(current, prev, bound.owner) || current.catalogGroup != bound.catalogGroup || !current.allowsRole("conversation") {
		return config.Provider{}, ErrAccountModelsUnavailable
	}
	resolved, err := p.ResolveModel(id, name)
	if err != nil {
		return config.Provider{}, err
	}
	if !p.credentialMatches(current, resolved, bound.owner) || resolved.Model != prev.Model || !current.permits(resolved.Model) {
		return config.Provider{}, ErrAccountModelsUnavailable
	}
	return resolved, nil
}

// RefreshCapabilities requests discovery on its existing owner loop.
func (p *Pool) SetCapabilitiesRefresher(refresh func(context.Context) error) {
	p.mu.Lock()
	p.capabilitiesRefresh = refresh
	p.mu.Unlock()
}

func (p *Pool) RefreshCapabilities(ctx context.Context) error {
	p.mu.Lock()
	refresh := p.capabilitiesRefresh
	p.mu.Unlock()
	if refresh == nil {
		return errors.New("provider: capability discovery is unavailable")
	}
	return refresh(ctx)
}

// InvalidateCapabilities retains catalog/selection identity during discovery
// failure while closing feature admission until a later successful negotiation.
func (p *Pool) InvalidateCapabilities() {
	p.invalidateCapabilities(false)
}

// InterruptCapabilityDiscovery closes admission after a transient discovery
// failure. A chat with a previously negotiated grant can request a fresh
// authorization check from the discovery owner before sending anything.
func (p *Pool) InterruptCapabilityDiscovery() {
	p.invalidateCapabilities(true)
}

var errCapabilityDiscoveryInterrupted = fmt.Errorf("%w: Gateway authorization discovery was interrupted", ErrAccountModelsUnavailable)

func (p *Pool) invalidateCapabilities(interrupted bool) {
	invalidate := func(source *AccountSource) *AccountSource {
		copy := *source
		copy.SetCapabilities(nil)
		copy.discoveryInterrupted = interrupted && (source.discoveryInterrupted || source.capabilities.Allows("conversation"))
		return &copy
	}
	changed := false
	if current := p.account.Load(); current != nil {
		// Narrow only the exact publication observed. Logout or a new device
		// can replace it concurrently; invalidation must never revive it.
		changed = p.account.CompareAndSwap(current, invalidate(current))
	}
	if current := p.localAccounts.Load(); current != nil {
		next := &localAccountList{}
		for _, source := range current.sources {
			next.sources = append(next.sources, invalidate(source))
		}
		changed = p.localAccounts.CompareAndSwap(current, next) || changed
	}
	if changed {
		p.Invalidate()
	}
}

// FeatureAvailable preserves independent LOCAL providers while requiring the
// selected account source's own role grant.
func (p *Pool) FeatureAvailable(id, role string) bool {
	if id == "" {
		id = p.DefaultProviderID()
	}
	if source := p.accountForExecution(id); source != nil {
		return source.allowsRole(role)
	}
	return !strings.HasPrefix(id, config.AccountSourcePrefix) && p.localExecution()
}

// AllowsPersonalFeatureModel admits an explicitly configured personal model
// under an editable account's grant for this capability.
func (p *Pool) AllowsPersonalFeatureModel(id, role string) bool {
	if id == "" || strings.HasPrefix(id, config.AccountSourcePrefix) {
		return false
	}
	src := p.activeAccount()
	if src == nil || src.managed || !src.allowsRole(role) ||
		src.negotiated && (!src.capabilities.Configurable || !src.capabilities.Capabilities[role].Configurable) {
		return false
	}
	row, ok := p.cfg.Provider(id)
	return ok && row.Listed()
}

type capabilityRoleKey struct{}

// RoleModelBuilder selects the role's declared public wire adapter. Conversation
// workers retain ModelBuilder and cannot borrow another role's endpoint grant.
func (p *Pool) RoleModelBuilder(ctx context.Context, id, name, effort string, rec Recorder, role string) (func(string, string) model.BaseChatModel, error) {
	if !p.mock && !p.FeatureAvailable(id, role) && (role != "translation" || !p.AllowsPersonalFeatureModel(id, role)) {
		return nil, ErrModelPolicy
	}
	factory, err := p.ModelBuilder(context.WithValue(ctx, capabilityRoleKey{}, role), id, name, effort, rec)
	if err != nil || p.mock {
		return factory, err
	}
	return func(_ string, agentID string) model.BaseChatModel { return factory(role, agentID) }, nil
}

func (p *Pool) SetCapabilitiesTrigger(trigger func()) {
	p.mu.Lock()
	p.capabilitiesTrigger = trigger
	p.mu.Unlock()
}

// A live grant rejection refreshes discovery without retrying the user's call.
func (p *Pool) capabilityResult(err error) {
	p.NotifyCapabilityRejection(HTTPStatus(err))
}

func (p *Pool) NotifyCapabilityRejection(status int) {
	if status != 401 && status != 403 {
		return
	}
	p.InvalidateCapabilities()
	p.triggerCapabilityRefresh()
}

func (p *Pool) triggerCapabilityRefresh() {
	p.mu.Lock()
	trigger := p.capabilitiesTrigger
	p.mu.Unlock()
	if trigger != nil {
		trigger()
	}
}

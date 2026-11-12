package provider

import (
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// AccountProfile is the signed-in server's official model agreement. It lives
// only in memory. The concrete model id stays on the server.
type AccountProfile struct {
	Label         string
	Instruction   string
	Tools         []string
	RestrictTools bool
	ContextWindow int
}

// ProfileTrace is the part of an official profile a turn may record. The
// instruction text stays out of it.
type ProfileTrace struct {
	Label            string   `json:"label"`
	Alias            string   `json:"alias"`
	Tools            []string `json:"tools,omitempty"`
	RestrictTools    bool     `json:"restrict_tools,omitempty"`
	InstructionBytes int      `json:"instruction_bytes"`
}

// NewAccountProfile projects one official profile. names from a catalog are
// not accepted here, so a profile cannot publish them by mistake.
func NewAccountProfile(id, baseURL, token string, profile AccountProfile) (*AccountSource, error) {
	src, err := NewAccountSource(id, profile.Label, baseURL, token, Catalog{
		Names: []string{config.OfficialModelAlias},
		Types: map[string]string{config.OfficialModelAlias: "chat"},
		Windows: func() map[string]int {
			if profile.ContextWindow <= 0 {
				return nil
			}
			return map[string]int{config.OfficialModelAlias: profile.ContextWindow}
		}(),
	})
	if err != nil {
		return nil, err
	}
	src.profiled = true
	src.profile = accountProfile{
		label:       strings.TrimSpace(profile.Label),
		instruction: strings.TrimSpace(profile.Instruction),
		tools:       append([]string(nil), profile.Tools...),
		restrict:    profile.RestrictTools,
		window:      profile.ContextWindow,
	}
	src.label = src.profile.label
	return src, nil
}

func (s *AccountSource) Profiled() bool {
	return s != nil && s.profiled
}

func (s *AccountSource) Instruction() string {
	if s == nil || !s.profiled {
		return ""
	}
	return s.profile.instruction
}

func (s *AccountSource) ToolAllow() (restrict bool, names []string) {
	if s == nil || !s.profiled || !s.profile.restrict {
		return false, nil
	}
	return true, append([]string(nil), s.profile.tools...)
}

func (s *AccountSource) profileTrace() (ProfileTrace, bool) {
	if s == nil || !s.profiled {
		return ProfileTrace{}, false
	}
	trace := ProfileTrace{
		Label:            s.profile.label,
		Alias:            config.OfficialModelAlias,
		InstructionBytes: len(s.profile.instruction),
		RestrictTools:    s.profile.restrict,
	}
	if s.profile.restrict {
		trace.Tools = append([]string(nil), s.profile.tools...)
	}
	return trace, true
}

func (s *AccountSource) profileRow(isDefault bool) Info {
	window := 0
	if s.profile.window > 0 {
		window = s.profile.window
	}
	return Info{
		ID: ChoiceID(s.id, config.OfficialModelAlias), ProviderID: s.id, ProviderLabel: s.profile.label,
		Label: s.profile.label, Model: config.OfficialModelAlias, Ready: true,
		Default: isDefault, DefaultFollowsSetting: isDefault,
		ContextWindow: window, Source: SourceAccount,
	}
}

// AccountInstruction is the official guidance for a turn on providerID.
// A full presentation leaves every other provider alone.
func (p *Pool) AccountInstruction(providerID string) string {
	src := p.profileSource(providerID)
	if src == nil {
		return ""
	}
	return src.Instruction()
}

// AccountToolPolicy is the workspace allowlist for a turn on providerID.
// restrict is false when the profile did not send a list.
func (p *Pool) AccountToolPolicy(providerID string) (restrict bool, names []string) {
	src := p.profileSource(providerID)
	if src == nil {
		return false, nil
	}
	return src.ToolAllow()
}

// AccountProfileTrace is the durable summary for a turn that uses the profile.
func (p *Pool) AccountProfileTrace(providerID string) (ProfileTrace, bool) {
	src := p.profileSource(providerID)
	if src == nil {
		return ProfileTrace{}, false
	}
	return src.profileTrace()
}

func (p *Pool) profileSource(providerID string) *AccountSource {
	if p == nil {
		return nil
	}
	src := p.account.Load()
	if src == nil || !src.profiled {
		return nil
	}
	providerID = strings.TrimSpace(providerID)
	if p.OfficialOnly() || providerID == src.ID() || (providerID == "" && p.DefaultProviderID() == src.ID()) {
		return src
	}
	return nil
}

func (s *AccountSource) sameProfile(o *AccountSource) bool {
	if s.profiled != o.profiled {
		return false
	}
	if !s.profiled {
		return true
	}
	if s.profile.label != o.profile.label || s.profile.instruction != o.profile.instruction || s.profile.restrict != o.profile.restrict || s.profile.window != o.profile.window || len(s.profile.tools) != len(o.profile.tools) {
		return false
	}
	for i, name := range s.profile.tools {
		if o.profile.tools[i] != name {
			return false
		}
	}
	return true
}

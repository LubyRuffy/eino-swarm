package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// Where a listed model comes from. The composer groups by it, and the signed-in
// account always sorts ahead of the user's own providers.
const (
	SourceAccount  = "account"
	SourceProvider = "provider"
)

// ErrAccountLoginRequired is what a conversation that picked a server model
// gets once the account is gone. The code is stable; nothing falls back to a
// provider that happens to have a model with the same name.
var ErrAccountLoginRequired = errors.New("account_login_required: sign in to the server again to use its models")

// AccountSource is the model catalog of the signed-in server account, held
// only in memory. It is never written to config.yaml, and the credential it
// carries exists only so this package can call the server: no accessor hands
// it out, and every printable form hides it.
type AccountSource struct {
	id      string
	label   string
	baseURL string
	token   string
	names   []string
	windows map[string]int
}

// NewAccountSource builds the projection of one account catalog. The id must
// use the reserved prefix so it can never collide with a configured provider.
func NewAccountSource(id, label, baseURL, token string, cat Catalog) (*AccountSource, error) {
	id, baseURL, token = strings.TrimSpace(id), strings.TrimSpace(baseURL), strings.TrimSpace(token)
	switch {
	case !strings.HasPrefix(id, config.AccountSourcePrefix):
		return nil, errors.New("provider: an account source id must use the reserved prefix")
	case baseURL == "":
		return nil, errors.New("provider: an account source needs its endpoint")
	case token == "":
		return nil, errors.New("provider: an account source needs its credential")
	case len(cat.Names) == 0:
		return nil, errors.New("provider: an account source needs at least one model")
	}
	windows := make(map[string]int, len(cat.Windows))
	for name, n := range cat.Windows {
		windows[name] = n
	}
	return &AccountSource{
		id: id, label: strings.TrimSpace(label), baseURL: baseURL, token: token,
		names: append([]string(nil), cat.Names...), windows: windows,
	}, nil
}

// ID is the provider id conversations store for this source.
func (s *AccountSource) ID() string { return s.id }

// String and GoString use value receivers so that a dereferenced copy, which
// fmt would otherwise print field by field, hides the credential as well.
func (s AccountSource) String() string {
	return fmt.Sprintf("account source %s (%d models)", s.id, len(s.names))
}

func (s AccountSource) GoString() string { return s.String() }

// provider is the runtime provider record. Its APIKey is the account
// credential and stays in Go: Provider.APIKey is not serialized.
func (s *AccountSource) provider() config.Provider {
	windows := make(map[string]int, len(s.windows))
	for name, n := range s.windows {
		windows[name] = n
	}
	return config.Provider{
		ID: s.id, Label: s.label, BaseURL: s.baseURL, APIKey: s.token,
		Model: s.names[0], Catalog: append([]string(nil), s.names...), ModelContext: windows,
	}
}

// ParseCatalog reads an OpenAI-compatible model list body.
func ParseCatalog(raw []byte) (Catalog, error) { return parseModelCatalog(raw) }

// rows lists the account's models for the composer; isDefault marks the first
// as the model a new conversation starts on.
func (s *AccountSource) rows(isDefault bool) []Info {
	out := make([]Info, 0, len(s.names))
	for i, name := range s.names {
		out = append(out, Info{
			ID: ChoiceID(s.id, name), ProviderID: s.id, ProviderLabel: s.label,
			Label: name, Model: name, Ready: true, Default: isDefault && i == 0,
			ContextWindow: s.windows[name], Source: SourceAccount,
		})
	}
	return out
}

// SetAccountSource publishes the signed-in account's models, or clears them
// with nil. Cached clients are dropped so a replaced credential is never used
// again. A scripted pool ignores it: offline runs never reach a network.
func (p *Pool) SetAccountSource(src *AccountSource) {
	if p.mock {
		return
	}
	if prev := p.account.Load(); prev.equal(src) {
		return
	}
	p.account.Store(src)
	p.Invalidate()
}

// AccountSourceID is the id of the published account source, empty when
// signed out.
func (p *Pool) AccountSourceID() string {
	if src := p.account.Load(); src != nil {
		return src.ID()
	}
	return ""
}

// DefaultProviderID is the provider a new conversation starts on: the
// account's models when that is the chosen source and they are available,
// otherwise the configured default provider.
func (p *Pool) DefaultProviderID() string {
	m := p.cfg.ModelsSnapshot()
	if m.DefaultSource != config.DefaultSourceProvider {
		if src := p.account.Load(); src != nil {
			return src.ID()
		}
	}
	return m.Default
}

// equal reports whether two projections would behave identically. A periodic
// refresh that finds nothing new must not drop the shared client every time.
func (s *AccountSource) equal(o *AccountSource) bool {
	if s == nil || o == nil {
		return s == o
	}
	if s.id != o.id || s.label != o.label || s.baseURL != o.baseURL || s.token != o.token || len(s.names) != len(o.names) || len(s.windows) != len(o.windows) {
		return false
	}
	for i, name := range s.names {
		if o.names[i] != name {
			return false
		}
	}
	for name, n := range s.windows {
		if o.windows[name] != n {
			return false
		}
	}
	return true
}

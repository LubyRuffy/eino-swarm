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
	types   map[string]string
}

// ModelKinds counts the server's classification. Empty type is unclassified.
// EmbeddingModels keeps those names in catalog order for the embedding picker.
type ModelKinds struct {
	Chat            int      `json:"chat"`
	Embedding       int      `json:"embedding"`
	OCR             int      `json:"ocr"`
	Decisions       int      `json:"decisions"`
	Other           int      `json:"other"`
	Unclassified    int      `json:"unclassified"`
	EmbeddingModels []string `json:"embedding_models,omitempty"`
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
	types := make(map[string]string, len(cat.Types))
	for _, name := range cat.Names {
		if kind := strings.TrimSpace(cat.Types[name]); kind != "" {
			types[name] = kind
		}
	}
	return &AccountSource{
		id: id, label: strings.TrimSpace(label), baseURL: baseURL, token: token,
		names: append([]string(nil), cat.Names...), windows: windows, types: types,
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
// conversationalNames is what a new chat can start on: chat, plus rows the
// server never classified. preferred selects one of those names when it is
// still in the list; otherwise the first one remains the default.
func (s *AccountSource) conversationalNames() []string {
	out := make([]string, 0, len(s.names))
	for _, name := range s.names {
		if conversationalModelType(s.types[name]) {
			out = append(out, name)
		}
	}
	return out
}

func chooseName(names []string, preferred string) string {
	preferred = strings.TrimSpace(preferred)
	for _, name := range names {
		if name == preferred {
			return name
		}
	}
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// Kinds is the classification the settings screens count and filter on.
func (s *AccountSource) Kinds() ModelKinds {
	var kinds ModelKinds
	for _, name := range s.names {
		switch strings.TrimSpace(s.types[name]) {
		case "":
			kinds.Unclassified++
		case "chat":
			kinds.Chat++
		case "embedding":
			kinds.Embedding++
			kinds.EmbeddingModels = append(kinds.EmbeddingModels, name)
		case "ocr":
			kinds.OCR++
		case "decisions":
			kinds.Decisions++
		default:
			kinds.Other++
		}
	}
	return kinds
}

func (s *AccountSource) provider(preferred string) config.Provider {
	names := s.conversationalNames()
	windows := make(map[string]int, len(names))
	for _, name := range names {
		if n := s.windows[name]; n > 0 {
			windows[name] = n
		}
	}
	model := strings.TrimSpace(preferred)
	if model == "" {
		model = chooseName(names, "")
	}
	return config.Provider{
		ID: s.id, Label: s.label, BaseURL: s.baseURL, APIKey: s.token,
		Model: model, Catalog: names, ModelContext: windows,
	}
}

// ConversationDefault is the model an unpinned conversation on id uses.
// followsSetting is false only for an account catalog whose saved account
// model is empty: name is then the current first conversation model, and the
// caller remembers it instead of reading the catalog again on the next send.
func (p *Pool) ConversationDefault(id string) (name string, followsSetting bool, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = p.DefaultProviderID()
	}
	if src := p.account.Load(); src != nil && id == src.ID() {
		if preferred := p.accountModel(); preferred != "" {
			return preferred, true, true
		}
		return chooseName(src.conversationalNames(), ""), false, true
	}
	prov, err := p.Resolve(id)
	if err != nil {
		return "", false, false
	}
	return strings.TrimSpace(prov.Model), true, true
}

// ParseCatalog reads an OpenAI-compatible model list body.
func ParseCatalog(raw []byte) (Catalog, error) { return parseModelCatalog(raw) }

// rows lists the conversation models. A saved preferred name stays the default
// after it leaves the catalog, as a not-ready row, so the composer does not
// paint a different model as selected. An empty preferred marks the current
// first conversation name, and that mark does not follow a setting.
func (s *AccountSource) rows(isDefault bool, preferred string) []Info {
	names := s.conversationalNames()
	preferred = strings.TrimSpace(preferred)
	follows := preferred != ""
	chosen := preferred
	chosenReady := false
	if !follows {
		chosen = chooseName(names, "")
		chosenReady = chosen != ""
	} else {
		for _, name := range names {
			if name == preferred {
				chosenReady = true
				break
			}
		}
	}
	out := make([]Info, 0, len(names)+1)
	for _, name := range names {
		out = append(out, Info{
			ID: ChoiceID(s.id, name), ProviderID: s.id, ProviderLabel: s.label,
			Label: name, Model: name, Ready: true,
			Default:               isDefault && name == chosen,
			DefaultFollowsSetting: isDefault && follows && name == chosen,
			ContextWindow:         s.windows[name], Source: SourceAccount,
		})
	}
	if isDefault && follows && chosen != "" && !chosenReady {
		out = append(out, Info{
			ID: ChoiceID(s.id, chosen), ProviderID: s.id, ProviderLabel: s.label,
			Label: chosen, Model: chosen, Ready: false, Default: true, DefaultFollowsSetting: true,
			Source: SourceAccount,
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
// account's conversation models when that is the chosen source and at least
// one can start a chat, otherwise the configured default provider.
func (p *Pool) DefaultProviderID() string {
	m := p.cfg.ModelsSnapshot()
	if m.DefaultSource != config.DefaultSourceProvider {
		if src := p.account.Load(); src != nil && len(src.conversationalNames()) > 0 {
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
	if s.id != o.id || s.label != o.label || s.baseURL != o.baseURL || s.token != o.token || len(s.names) != len(o.names) || len(s.windows) != len(o.windows) || len(s.types) != len(o.types) {
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
	for name, kind := range s.types {
		if o.types[name] != kind {
			return false
		}
	}
	return true
}

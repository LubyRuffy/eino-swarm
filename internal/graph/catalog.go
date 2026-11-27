// Package graph is the pure half of the graph kernel: the subject kind
// catalog, identity normalization and attribute validation. It knows nothing
// about storage, so the same rules serve the model tools, the HTTP surface and
// the tests. The store package persists what this package accepts.
package graph

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/frontend"
)

// Normalizers is the fixed set a catalog entry may name. Adding a subject kind
// needs no Go change unless it needs a rule that is not here.
var Normalizers = []string{"ip", "domain", "url", "host", "repo", "repo_path", "query", "email", "identifier", "text", "asset"}

// Attribute value types a catalog entry may declare.
const (
	TypeString = "string"
	TypeText   = "text"
	TypeInt    = "int"
	TypeList   = "list"
)

// Default value budgets, in characters. A catalog attribute can lower or raise
// its own with Max.
const (
	DefaultStringMax = 512
	DefaultTextMax   = 2000
	MaxListItems     = 32
)

// AttrDef declares one attribute of a subject kind.
type AttrDef struct {
	Name  string            `json:"name"`
	Type  string            `json:"type"`
	Label map[string]string `json:"label"`
	// Max is a character limit for string, text and list items, and the upper
	// bound for an int when MaxValue is unset.
	Max      int  `json:"max,omitempty"`
	Min      *int `json:"min,omitempty"`
	MaxValue *int `json:"max_value,omitempty"`
	// Secret marks a credential: stored as written, never shown in digests,
	// logs, backups that omit credentials or the phone.
	Secret bool `json:"secret,omitempty"`
	// StripUserinfoTo moves credentials embedded in an address value into the
	// named attribute, so the address itself can be displayed.
	StripUserinfoTo string `json:"strip_userinfo_to,omitempty"`
	// DerivePathTo fills the named attribute with the repository path of this
	// address when that attribute is blank.
	DerivePathTo string `json:"derive_path_to,omitempty"`
}

// KeyDef is one way a subject of a kind is recognised. A key is made of one
// or more attributes; a list attribute yields one key per element.
type KeyDef struct {
	Name       string   `json:"name"`
	Normalizer string   `json:"normalizer"`
	Parts      []string `json:"parts"`
	// Required lists parts that must be present for the key to exist. A key
	// with a single part requires it.
	Required []string `json:"required,omitempty"`
	// OnlyWithout suppresses this key while any named key can be derived, so a
	// stronger identifier replaces a weaker fallback.
	OnlyWithout []string `json:"only_without,omitempty"`
}

// Kind is one entry of the subject catalog.
type Kind struct {
	ID          string            `json:"id"`
	Name        map[string]string `json:"name"`
	Description string            `json:"description"`
	// Display lists attribute names or {attr} templates; the first whose
	// fields are all present names the subject.
	Display []string  `json:"display"`
	Attrs   []AttrDef `json:"attrs"`
	Keys    []KeyDef  `json:"keys"`
}

// Catalog is the loaded, validated kind registry.
type Catalog struct {
	Version int    `json:"version"`
	Other   string `json:"other"`
	Kinds   []Kind `json:"kinds"`

	byID map[string]*Kind
}

var kindID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ParseCatalog decodes and validates a catalog document.
func ParseCatalog(raw []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("graph: decode catalog: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
	defaultErr     error
)

// Default returns the catalog embedded with the application.
func Default() (*Catalog, error) {
	defaultOnce.Do(func() { defaultCatalog, defaultErr = ParseCatalog(frontend.GraphKinds()) })
	return defaultCatalog, defaultErr
}

// MustDefault is for callers that cannot continue without the catalog; a test
// pins that the embedded document always parses.
func MustDefault() *Catalog {
	c, err := Default()
	if err != nil {
		panic(err)
	}
	return c
}

func (c *Catalog) validate() error {
	c.byID = make(map[string]*Kind, len(c.Kinds))
	for i := range c.Kinds {
		k := &c.Kinds[i]
		if !kindID.MatchString(k.ID) {
			return fmt.Errorf("graph: catalog kind id %q is not lower snake case", k.ID)
		}
		if c.byID[k.ID] != nil {
			return fmt.Errorf("graph: catalog kind %q is declared twice", k.ID)
		}
		if k.Name["en"] == "" || k.Name["zh"] == "" {
			return fmt.Errorf("graph: kind %q needs an en and a zh name", k.ID)
		}
		if err := k.validate(); err != nil {
			return err
		}
		c.byID[k.ID] = k
	}
	if c.byID[c.Other] == nil {
		return fmt.Errorf("graph: catalog has no generic kind %q", c.Other)
	}
	return nil
}

func (k *Kind) validate() error {
	attrs := map[string]*AttrDef{}
	for i := range k.Attrs {
		a := &k.Attrs[i]
		if a.Name == "" || attrs[a.Name] != nil {
			return fmt.Errorf("graph: kind %q has an empty or repeated attribute %q", k.ID, a.Name)
		}
		if !slices.Contains([]string{TypeString, TypeText, TypeInt, TypeList}, a.Type) {
			return fmt.Errorf("graph: kind %q attribute %q has unknown type %q", k.ID, a.Name, a.Type)
		}
		if a.Label["en"] == "" || a.Label["zh"] == "" {
			return fmt.Errorf("graph: kind %q attribute %q needs en and zh labels", k.ID, a.Name)
		}
		attrs[a.Name] = a
	}
	for _, a := range k.Attrs {
		for _, target := range []string{a.StripUserinfoTo, a.DerivePathTo} {
			if target != "" && attrs[target] == nil {
				return fmt.Errorf("graph: kind %q attribute %q writes to unknown attribute %q", k.ID, a.Name, target)
			}
		}
	}
	if len(k.Keys) == 0 {
		return fmt.Errorf("graph: kind %q has no identity key", k.ID)
	}
	keys := map[string]bool{}
	for _, key := range k.Keys {
		if key.Name == "" || keys[key.Name] {
			return fmt.Errorf("graph: kind %q has an empty or repeated key %q", k.ID, key.Name)
		}
		keys[key.Name] = true
		if !slices.Contains(Normalizers, key.Normalizer) {
			return fmt.Errorf("graph: kind %q key %q uses unknown normalizer %q", k.ID, key.Name, key.Normalizer)
		}
		if len(key.Parts) == 0 || (len(key.Parts) > 1) != (key.Normalizer == "asset") {
			return fmt.Errorf("graph: kind %q key %q: only the asset normalizer takes several parts", k.ID, key.Name)
		}
		for _, p := range append(slices.Clone(key.Parts), key.Required...) {
			if attrs[p] == nil {
				return fmt.Errorf("graph: kind %q key %q names unknown attribute %q", k.ID, key.Name, p)
			}
		}
		for _, r := range key.Required {
			if !slices.Contains(key.Parts, r) {
				return fmt.Errorf("graph: kind %q key %q requires %q which is not one of its parts", k.ID, key.Name, r)
			}
		}
	}
	for _, key := range k.Keys {
		for _, other := range key.OnlyWithout {
			if !keys[other] || other == key.Name {
				return fmt.Errorf("graph: kind %q key %q is suppressed by unknown key %q", k.ID, key.Name, other)
			}
		}
	}
	if len(k.Display) == 0 {
		return fmt.Errorf("graph: kind %q has no display rule", k.ID)
	}
	for _, d := range k.Display {
		for _, field := range displayFields(d) {
			if attrs[field] == nil {
				return fmt.Errorf("graph: kind %q display rule %q names unknown attribute %q", k.ID, d, field)
			}
		}
	}
	return nil
}

// Kind returns the entry for id, or nil.
func (c *Catalog) Kind(id string) *Kind { return c.byID[id] }

// IDs lists the registered kind ids in catalog order.
func (c *Catalog) IDs() []string {
	ids := make([]string, len(c.Kinds))
	for i, k := range c.Kinds {
		ids[i] = k.ID
	}
	return ids
}

// Attr returns the declaration of one attribute, or nil.
func (k *Kind) Attr(name string) *AttrDef {
	for i := range k.Attrs {
		if k.Attrs[i].Name == name {
			return &k.Attrs[i]
		}
	}
	return nil
}

// SecretAttrs names the credential attributes of the kind.
func (k *Kind) SecretAttrs() []string {
	var names []string
	for _, a := range k.Attrs {
		if a.Secret {
			names = append(names, a.Name)
		}
	}
	return names
}

// Describe renders the kind list for the update_graph parameter definition.
// The list lives there, never in the prompt. Kind prose stays out of this
// string: it is sent on every model call, and the attribute names are enough
// to choose a kind.
func (c *Catalog) Describe() string {
	var b strings.Builder
	for _, k := range c.Kinds {
		fmt.Fprintf(&b, "- %s (%s):", k.ID, k.Name["en"])
		for i, a := range k.Attrs {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(" " + a.Name)
			if a.Type == TypeInt || a.Type == TypeList {
				b.WriteString("(" + a.Type + ")")
			}
		}
		b.WriteString(". Identity:")
		for i, key := range k.Keys {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(" " + strings.Join(key.Parts, "+"))
			if len(key.OnlyWithout) > 0 {
				b.WriteString(" when no " + strings.Join(key.OnlyWithout, "/"))
			}
		}
		b.WriteString(".\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func displayFields(rule string) []string {
	if !strings.Contains(rule, "{") {
		return []string{rule}
	}
	var out []string
	for _, part := range strings.Split(rule, "{")[1:] {
		if end := strings.IndexByte(part, '}'); end > 0 {
			out = append(out, part[:end])
		}
	}
	return out
}

// DisplayName names a subject from its attributes, falling back to its id.
func (k *Kind) DisplayName(attrs map[string]any) string {
	for _, rule := range k.Display {
		name, ok := rule, true
		if strings.Contains(rule, "{") {
			for _, field := range displayFields(rule) {
				value := attrString(attrs[field])
				if value == "" {
					ok = false
					break
				}
				name = strings.ReplaceAll(name, "{"+field+"}", value)
			}
		} else if name = attrString(attrs[rule]); name == "" {
			ok = false
		}
		if ok {
			return name
		}
	}
	return ""
}

func attrString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []string:
		if len(x) > 0 {
			return x[0]
		}
	case []any:
		if len(x) > 0 {
			if s, ok := x[0].(string); ok {
				return s
			}
		}
	case int:
		return fmt.Sprint(x)
	case int64:
		return fmt.Sprint(x)
	case float64:
		return fmt.Sprint(int64(x))
	}
	return ""
}

// SortedAttrNames is a stable iteration order for rendering and hashing.
func SortedAttrNames(attrs map[string]any) []string {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

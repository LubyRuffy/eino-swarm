package graph

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// KeyValue is one identity key of a subject: which key, and its normalized
// value. A subject can hold several.
type KeyValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Clean validates incoming attributes against the kind and returns them in
// stored form: known names only, trimmed values, lists capped, credentials
// moved out of addresses and derived attributes filled.
func (k *Kind) Clean(attrs map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(attrs))
	for _, name := range SortedAttrNames(attrs) {
		def := k.Attr(name)
		if def == nil {
			return nil, invalid("kind %s has no attribute %q", k.ID, name)
		}
		value, err := coerce(def, attrs[name])
		if err != nil {
			return nil, err
		}
		if value != nil {
			out[name] = value
		}
	}
	for _, def := range k.Attrs {
		address, ok := out[def.Name].(string)
		if !ok {
			continue
		}
		if def.StripUserinfoTo != "" {
			cleaned, credential := stripUserinfo(address)
			out[def.Name] = cleaned
			if _, has := out[def.StripUserinfoTo]; !has && credential != "" {
				out[def.StripUserinfoTo] = credential
			}
		}
		if def.DerivePathTo != "" {
			if _, has := out[def.DerivePathTo]; !has {
				if path := RepoPath(out[def.Name].(string)); path != "" {
					out[def.DerivePathTo] = path
				}
			}
		}
	}
	return out, nil
}

func stripUserinfo(address string) (cleaned, credential string) {
	if strings.Contains(address, "://") {
		u, err := url.Parse(address)
		if err != nil || u.User == nil {
			return address, ""
		}
		credential = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			credential += ":" + pw
		}
		u.User = nil
		return u.String(), credential
	}
	// user@host:path is the ssh login convention, not a secret.
	if m := scpAddress.FindStringSubmatch(address); m != nil {
		return m[1] + ":" + m[2], ""
	}
	return address, ""
}

func coerce(def *AttrDef, raw any) (any, error) {
	switch def.Type {
	case TypeInt:
		n, ok, err := toInt(raw)
		if err != nil {
			return nil, invalid("attribute %s: %v", def.Name, err)
		}
		if !ok {
			return nil, nil
		}
		if def.Min != nil && n < int64(*def.Min) || def.MaxValue != nil && n > int64(*def.MaxValue) {
			return nil, invalid("attribute %s is out of range", def.Name)
		}
		return n, nil
	case TypeList:
		items, err := toStrings(raw)
		if err != nil {
			return nil, invalid("attribute %s: %v", def.Name, err)
		}
		var out []string
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" || slices.Contains(out, item) {
				continue
			}
			if utf8.RuneCountInString(item) > limit(def, DefaultStringMax) {
				return nil, invalid("attribute %s has an item longer than %d characters", def.Name, limit(def, DefaultStringMax))
			}
			out = append(out, item)
		}
		if len(out) > MaxListItems {
			return nil, invalid("attribute %s holds at most %d items", def.Name, MaxListItems)
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	}
	items, err := toStrings(raw)
	if err != nil || len(items) > 1 {
		return nil, invalid("attribute %s takes one text value", def.Name)
	}
	if len(items) == 0 {
		return nil, nil
	}
	text := strings.TrimSpace(items[0])
	if text == "" {
		return nil, nil
	}
	fallback := DefaultStringMax
	if def.Type == TypeText {
		fallback = DefaultTextMax
	}
	if utf8.RuneCountInString(text) > limit(def, fallback) {
		return nil, invalid("attribute %s is longer than %d characters", def.Name, limit(def, fallback))
	}
	return text, nil
}

func limit(def *AttrDef, fallback int) int {
	if def.Max > 0 {
		return def.Max
	}
	return fallback
}

func toInt(raw any) (int64, bool, error) {
	switch x := raw.(type) {
	case nil:
		return 0, false, nil
	case int:
		return int64(x), true, nil
	case int64:
		return x, true, nil
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > 1<<53 {
			return 0, false, fmt.Errorf("must be a whole number")
		}
		return int64(x), true, nil
	case json.Number:
		n, err := x.Int64()
		return n, err == nil, err
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, false, nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("must be a whole number")
		}
		return n, true, nil
	}
	return 0, false, fmt.Errorf("must be a whole number")
}

func toStrings(raw any) ([]string, error) {
	switch x := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{x}, nil
	case float64, int, int64, json.Number:
		return []string{fmt.Sprint(x)}, nil
	case []string:
		return x, nil
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, err := toStrings(item)
			if err != nil || len(s) != 1 {
				return nil, fmt.Errorf("must be text")
			}
			out = append(out, s[0])
		}
		return out, nil
	}
	return nil, fmt.Errorf("must be text")
}

// IdentityKeys returns the identity keys of a cleaned attribute set. A fallback key
// is dropped while the stronger key it yields to can be derived. A set that
// yields no key cannot identify a subject and is refused.
func (k *Kind) IdentityKeys(attrs map[string]any) ([]KeyValue, error) {
	derived := make(map[string][]string, len(k.Keys))
	for _, key := range k.Keys {
		values, err := deriveKey(k, key, attrs)
		if err != nil {
			return nil, err
		}
		if len(values) > 0 {
			derived[key.Name] = values
		}
	}
	var out []KeyValue
	for _, key := range k.Keys {
		if slices.ContainsFunc(key.OnlyWithout, func(other string) bool { return len(derived[other]) > 0 }) {
			continue
		}
		for _, v := range derived[key.Name] {
			out = append(out, KeyValue{Name: key.Name, Value: v})
		}
	}
	if len(out) == 0 {
		return nil, invalid("kind %s needs %s to be recognised", k.ID, k.identityHint())
	}
	return out, nil
}

func (k *Kind) identityHint() string {
	var options []string
	for _, key := range k.Keys {
		need := key.Required
		if len(need) == 0 {
			need = key.Parts
		}
		options = append(options, strings.Join(need, "+"))
	}
	return strings.Join(options, " or ")
}

func deriveKey(k *Kind, key KeyDef, attrs map[string]any) ([]string, error) {
	need := key.Required
	if len(need) == 0 {
		need = key.Parts
	}
	for _, p := range need {
		if attrString(attrs[p]) == "" {
			return nil, nil
		}
	}
	text := func(part string) string { return attrString(attrs[part]) }
	if key.Normalizer == "asset" {
		v, err := NormalizeAsset(text("ip"), text("port"), text("protocol"), text("host"))
		if err != nil {
			return nil, err
		}
		return []string{v}, nil
	}
	var raw []string
	switch v := attrs[key.Parts[0]].(type) {
	case []string:
		raw = v
	case string:
		raw = []string{v}
	case int64:
		raw = []string{strconv.FormatInt(v, 10)}
	}
	var out []string
	for _, item := range raw {
		v, err := Normalize(key.Normalizer, item)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// CheckKeyTransition refuses a change that would turn a subject into another
// one: every stored key must survive, unless a stronger key now supersedes it.
func (k *Kind) CheckKeyTransition(stored, next []KeyValue) error {
	for _, s := range stored {
		if slices.Contains(next, s) {
			continue
		}
		var def *KeyDef
		for i := range k.Keys {
			if k.Keys[i].Name == s.Name {
				def = &k.Keys[i]
			}
		}
		if def != nil && slices.ContainsFunc(def.OnlyWithout, func(o string) bool {
			return slices.ContainsFunc(next, func(n KeyValue) bool { return n.Name == o })
		}) {
			continue
		}
		return invalid("%s cannot change: that would be a different subject; write the new one separately", s.Name)
	}
	return nil
}

// MergeAdditive fills blank attributes and unions lists. A different value
// for an attribute that already has one is kept as is and reported, because
// replacing it needs the subject's current version.
func (k *Kind) MergeAdditive(existing, incoming map[string]any) (merged map[string]any, conflicts []string) {
	merged = cloneAttrs(existing)
	for _, name := range SortedAttrNames(incoming) {
		cur, has := merged[name]
		switch next := incoming[name].(type) {
		case []string:
			list, _ := cur.([]string)
			for _, item := range next {
				if !slices.Contains(list, item) && len(list) < MaxListItems {
					list = append(list, item)
				}
			}
			merged[name] = list
		default:
			if !has {
				merged[name] = next
			} else if !reflect.DeepEqual(cur, next) {
				conflicts = append(conflicts, name)
			}
		}
	}
	return merged, conflicts
}

// Overwrite replaces the named attributes. It is used only when the caller
// presented the current version.
func (k *Kind) Overwrite(existing, incoming map[string]any) map[string]any {
	merged := cloneAttrs(existing)
	for name, value := range incoming {
		merged[name] = value
	}
	return merged
}

func cloneAttrs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for name, value := range in {
		if list, ok := value.([]string); ok {
			value = slices.Clone(list)
		}
		out[name] = value
	}
	return out
}

// Normalized lists the stored form of attribute values after a JSON round
// trip, where lists come back as []any and numbers as float64.
func (k *Kind) Normalized(attrs map[string]any) map[string]any {
	out := make(map[string]any, len(attrs))
	for name, value := range attrs {
		def := k.Attr(name)
		if def == nil {
			continue
		}
		if cleaned, err := coerce(def, value); err == nil && cleaned != nil {
			out[name] = cleaned
		}
	}
	return out
}

// CleanLinkAttrs bounds the free-form attributes of a link: a handful of
// short text values, such as a stake or a note.
func CleanLinkAttrs(attrs map[string]any) (map[string]any, error) {
	if len(attrs) > MaxLinkAttrs {
		return nil, invalid("a link holds at most %d attributes", MaxLinkAttrs)
	}
	out := make(map[string]any, len(attrs))
	for _, name := range SortedAttrNames(attrs) {
		name := strings.TrimSpace(name)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			return nil, invalid("link attribute names are 1 to 64 characters")
		}
		items, err := toStrings(attrs[name])
		if err != nil || len(items) != 1 {
			return nil, invalid("link attribute %s takes one text or number value", name)
		}
		if text := strings.TrimSpace(items[0]); text != "" {
			out[name] = text
		}
	}
	if raw, _ := json.Marshal(out); len(raw) > MaxLinkAttrSize {
		return nil, invalid("link attributes are larger than %d bytes", MaxLinkAttrSize)
	}
	return out, nil
}

// Redact returns a copy of attrs without the credential attributes of the
// kind, and the names of the credentials that were present. Digests, logs, the
// phone and credential-free backups use it; the model reading one subject by
// id is the only reader that sees the values.
func (c *Catalog) Redact(kindID string, attrs map[string]any) (map[string]any, []string) {
	k := c.Kind(kindID)
	if k == nil {
		return attrs, nil
	}
	secrets := k.SecretAttrs()
	if len(secrets) == 0 {
		return attrs, nil
	}
	out := make(map[string]any, len(attrs))
	var present []string
	for name, value := range attrs {
		if slices.Contains(secrets, name) {
			present = append(present, name)
			continue
		}
		out[name] = value
	}
	slices.Sort(present)
	return out, present
}

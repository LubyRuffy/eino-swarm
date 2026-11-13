package config

import (
	"errors"
	"reflect"
	"strings"
)

const (
	// ProductModeFOFA is the install that signs in for models and does not
	// edit this machine's own providers. It is the default and is not written
	// into the file.
	ProductModeFOFA = "fofa"
	// ProductModeLocal shows model settings and the composer model menu.
	ProductModeLocal = "local"
)

// ErrInvalidProductMode is a product.mode value other than fofa or local.
var ErrInvalidProductMode = errors.New("config: product.mode must be fofa or local")

// ProductConfig is the install-wide mode. An empty Mode is FOFA and is omitted
// from the file so a default install does not advertise the switch.
type ProductConfig struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// Resolved is fofa or local. Anything that is not local, including empty, is fofa.
func (p ProductConfig) Resolved() string {
	if strings.EqualFold(strings.TrimSpace(p.Mode), ProductModeLocal) {
		return ProductModeLocal
	}
	return ProductModeFOFA
}

// FOFA reports the default install. Empty and an explicit fofa are the same.
func (p ProductConfig) FOFA() bool { return p.Resolved() == ProductModeFOFA }

func (c *Config) validateProduct() error {
	switch strings.ToLower(strings.TrimSpace(c.Product.Mode)) {
	case "", ProductModeFOFA, ProductModeLocal:
		return nil
	default:
		return ErrInvalidProductMode
	}
}

func (c *Config) normalizeProduct() {
	if strings.EqualFold(strings.TrimSpace(c.Product.Mode), ProductModeLocal) {
		c.Product.Mode = ProductModeLocal
		return
	}
	c.Product.Mode = ""
}

// SameModelSettings reports whether two model sections would change what the
// install runs. Nil and empty lists are the same, and a missing enabled flag
// is the same as listed. Provider order is part of the setting.
func SameModelSettings(a, b ModelsConfig) bool {
	if a.Default != b.Default || a.DefaultSource != b.DefaultSource || a.AccountModel != b.AccountModel {
		return false
	}
	if a.Presentation() != b.Presentation() {
		return false
	}
	if len(a.Providers) != len(b.Providers) {
		return false
	}
	for i := range a.Providers {
		if !sameProviderSettings(a.Providers[i], b.Providers[i]) {
			return false
		}
	}
	return true
}

func sameProviderSettings(a, b Provider) bool {
	return a.ID == b.ID &&
		a.Label == b.Label &&
		strings.TrimSpace(a.BaseURL) == strings.TrimSpace(b.BaseURL) &&
		a.APIKey == b.APIKey &&
		a.Model == b.Model &&
		a.APIMode == b.APIMode &&
		a.TimeoutSeconds == b.TimeoutSeconds &&
		a.FirstByteTimeoutSeconds == b.FirstByteTimeoutSeconds &&
		a.ContextWindow == b.ContextWindow &&
		a.Listed() == b.Listed() &&
		sameSlice(a.Catalog, b.Catalog) &&
		sameSlice(a.BuiltinTools, b.BuiltinTools) &&
		sameMap(a.ModelContext, b.ModelContext)
}

func sameSlice[T any](a, b []T) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func sameMap[K comparable, V any](a, b map[K]V) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

package config

import (
	"errors"
	"strings"
)

const (
	// DefaultSourceAccount starts new conversations on the signed-in server
	// account's models whenever they are available.
	DefaultSourceAccount = "account"
	// DefaultSourceProvider starts them on the default configured provider.
	DefaultSourceProvider = "provider"

	// AccountSourcePrefix opens every id of a model source projected from the
	// signed-in account. Those sources exist only at runtime, so a configured
	// provider may not use the prefix.
	AccountSourcePrefix = "gateway:"
)

// ParseDefaultSource validates a stored or submitted default model source.
// Empty means "not chosen yet" and is settled by settleDefaultSource.
func ParseDefaultSource(raw string) (string, error) {
	source := strings.ToLower(strings.TrimSpace(raw))
	switch source {
	case "", DefaultSourceAccount, DefaultSourceProvider:
		return source, nil
	default:
		return "", errors.New("default_source must be account or provider")
	}
}

const maxAccountModelLen = 256

func (c *Config) validateDefaultSource() error {
	source, err := ParseDefaultSource(c.Models.DefaultSource)
	if err != nil {
		return err
	}
	c.Models.DefaultSource = source
	c.Models.AccountModel = strings.TrimSpace(c.Models.AccountModel)
	if len(c.Models.AccountModel) > maxAccountModelLen {
		return errors.New("account_model is too long")
	}
	return nil
}

// settleDefaultSource gives a configuration that never chose a source its
// starting value. Someone who already runs a working provider (an older file,
// or OPENAI_* variables on first run) keeps it first; an empty setup follows
// the signed-in account. It runs after the provider list is normalized.
func (c *Config) settleDefaultSource() {
	if c.Models.DefaultSource != "" {
		return
	}
	c.Models.DefaultSource = DefaultSourceAccount
	if i := c.providerIndex(c.Models.Default); i >= 0 && c.Models.Providers[i].Ready() {
		c.Models.DefaultSource = DefaultSourceProvider
	}
}

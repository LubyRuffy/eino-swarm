package config

import (
	"errors"
	"strings"
)

// RemoteAccountLogoutTokens returns only the current origin's account receipt
// and this device's retained candidates. Callers must never expose these values.
func (c *Config) RemoteAccountLogoutTokens(origin, publicKey string) ([]string, error) {
	mu := c.mutex()
	mu.RLock()
	defer mu.RUnlock()
	current, err := RemoteOrigin(c.Remote.HubURL)
	if err != nil || current != origin || c.Remote.AuthMode != RemoteAuthNOSEC {
		return nil, errors.New("remote: account configuration changed")
	}
	credential, err := c.ReadRemoteCredential()
	if err != nil {
		return nil, err
	}
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return nil, err
	}
	var tokens []string
	add := func(token string) error {
		if token == "" || len(token) > 256 || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\x00") {
			return errors.New("remote: invalid private logout credential")
		}
		for _, previous := range tokens {
			if previous == token {
				return nil
			}
		}
		tokens = append(tokens, token)
		return nil
	}
	if credential.Authorized && credential.Origin == origin {
		if err := add(credential.Token); err != nil {
			return nil, err
		}
	}
	for _, candidate := range candidates {
		if candidate.Credential.Origin == origin && candidate.Credential.PublicKey == publicKey {
			if err := add(candidate.Credential.Token); err != nil {
				return nil, err
			}
		}
	}
	return tokens, nil
}

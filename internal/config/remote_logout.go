package config

import (
	"errors"
	"fmt"
	"os"
)

// ClearRemoteAccountTokens refuses to delete a replacement receipt or candidate
// that was not covered by this logout's remote acknowledgements.
func (c *Config) ClearRemoteAccountTokens(origin, publicKey string, tokens []string) error {
	allowed := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		allowed[token] = true
	}
	mu := c.mutex()
	mu.Lock()
	defer mu.Unlock()
	current, err := RemoteOrigin(c.Remote.HubURL)
	if err != nil || current != origin || c.Remote.AuthMode != RemoteAuthNOSEC {
		return errors.New("remote: account configuration changed")
	}
	credential, err := c.ReadRemoteCredential()
	if err != nil {
		return fmt.Errorf("remote: read account credential: %w", err)
	}
	candidates, err := c.pendingRemoteCandidates()
	if err != nil {
		return fmt.Errorf("remote: read retained login proofs: %w", err)
	}
	if credential.Authorized && credential.Origin == origin && !allowed[credential.Token] {
		return errors.New("remote: account credential changed during logout")
	}
	for _, candidate := range candidates {
		if candidate.Credential.Origin == origin && candidate.Credential.PublicKey == publicKey && !allowed[candidate.Credential.Token] {
			return errors.New("remote: retained login proof changed during logout")
		}
	}
	changed := false
	if credential.Authorized && credential.Origin == origin {
		if err := os.Remove(c.remoteTokenPath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remote: remove account credential: %w", err)
		}
		changed = true
	}
	for _, candidate := range candidates {
		if candidate.Credential.Origin != origin || candidate.Credential.PublicKey != publicKey {
			continue
		}
		if err := c.RemovePendingRemoteToken(candidate.Path); err != nil {
			return fmt.Errorf("remote: remove retained login proof: %w", err)
		}
		changed = true
	}
	if changed {
		if err := syncRemoteDirectory(c.RemoteDir()); err != nil {
			return fmt.Errorf("remote: persist account removal: %w", err)
		}
	}
	return nil
}

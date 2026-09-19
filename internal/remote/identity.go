package remote

import (
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
)

func loadIdentity(cfg *config.Config) (*crypto.Identity, error) {
	raw, err := cfg.RemoteIdentity()
	if err != nil {
		return nil, err
	}
	if len(raw) == crypto.KeySize {
		return crypto.FromPrivate(raw)
	}
	id, err := crypto.Generate()
	if err != nil {
		return nil, err
	}
	if err := cfg.WriteRemoteIdentity(id.Private()); err != nil {
		return nil, fmt.Errorf("remote: persist identity: %w", err)
	}
	return id, nil
}

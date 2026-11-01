package main

import (
	"bytes"
	"context"

	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	pstore "github.com/LubyRuffy/pairlink/store"
)

// The dependency's append-only Memory returns the first historical peer pair,
// including a revoked row. Gateway storage resolves the live replacement. The
// fixture keeps the real relay/Noise path while modelling that same lookup.
type fixtureBindings struct{ *pstore.Memory }

func newFixtureBindings() *fixtureBindings { return &fixtureBindings{Memory: pstore.NewMemory()} }

func (s *fixtureBindings) BindingByPeers(ctx context.Context, host, device []byte) (pstore.Binding, error) {
	rows, err := s.ListAllBindings(ctx)
	if err != nil {
		return pstore.Binding{}, err
	}
	for index := len(rows) - 1; index >= 0; index-- {
		row := rows[index]
		if !row.Revoked && bytes.Equal(row.HostPub, host) && bytes.Equal(row.DevicePub, device) {
			return row, nil
		}
	}
	return pstore.Binding{}, pstore.ErrNotFound
}

func (f *fixture) retirePhoneBindings(ctx context.Context, encodedPub string) error {
	if f.bindings == nil {
		return nil
	}
	pub, err := linkcrypto.ParsePublic(encodedPub)
	if err != nil {
		return err
	}
	rows, err := f.bindings.ListBindings(ctx, f.hostPub)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if bytes.Equal(row.DevicePub, pub) {
			if err := f.bindings.RevokeBinding(ctx, row.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *fixture) observeBindings(value map[string]any) {
	active, retired := 0, 0
	if f.bindings != nil {
		rows, _ := f.bindings.ListAllBindings(context.Background())
		for _, row := range rows {
			if row.Revoked {
				retired++
			} else {
				active++
			}
		}
	}
	value["active_bindings"], value["retired_bindings"] = active, retired
	value["host_online"], value["mobile_online"] = false, false
	if f.relay != nil {
		value["host_online"] = f.relay.PeerOnline(f.hostPub)
		if f.flow != nil {
			if pub, err := linkcrypto.ParsePublic(f.flow.DevicePub); err == nil {
				value["mobile_online"] = f.relay.PeerOnline(pub)
			}
		}
	}
}

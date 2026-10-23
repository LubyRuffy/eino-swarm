//go:build !darwin

package main

import (
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

func configureDesktopRecovery(opts desktop.Options, _ []string) (desktop.Options, func(), error) {
	return opts, func() {}, nil
}
func runDesktopAttach([]string) error { return errors.New("desktop-attach is supported only on macOS") }

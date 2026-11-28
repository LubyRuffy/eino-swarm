//go:build !linux

package main

import (
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

func runLinuxSupervisedEngine([]string) error { return errors.New("host supervision requires Linux") }

func holdStandaloneEngineAdmission(string) (func(), error) { return func() {}, nil }

func discoverSupervisedEngine(string, bool) (lease.Record, bool, error) {
	return lease.Record{}, false, nil
}

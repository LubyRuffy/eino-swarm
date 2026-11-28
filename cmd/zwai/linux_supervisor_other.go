//go:build !linux

package main

import "errors"

func runLinuxHostSupervisor([]string) error {
	return errors.New("host supervision is available on Linux")
}

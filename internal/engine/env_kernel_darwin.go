//go:build darwin

package engine

import (
	"context"
	"syscall"
)

func platformUname(ctx context.Context) ([]byte, error) {
	return readDarwinKernel(ctx, syscall.Sysctl)
}

// Read the same facts as uname without starting a process during turn admission.
// Sysctl has no cancellable API; check the existing context around its reads.
func readDarwinKernel(ctx context.Context, read func(string) (string, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kind, err := read("kern.ostype")
	if err != nil {
		return nil, err
	}
	release, err := read("kern.osrelease")
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(kind + " " + release), nil
}

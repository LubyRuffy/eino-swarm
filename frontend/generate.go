//go:build ignore

// Builds frontend/dist when the TypeScript sources changed.
// `go generate ./frontend` and `make frontend` run this.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/LubyRuffy/eino-swarm/frontend"
)

func main() {
	if err := frontend.Ensure(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

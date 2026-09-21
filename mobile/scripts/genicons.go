//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		next := filepath.Dir(root)
		if next == root {
			fmt.Fprintln(os.Stderr, "go.mod not found")
			os.Exit(1)
		}
		root = next
	}
	if err := desktop.WritePhoneIcons(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

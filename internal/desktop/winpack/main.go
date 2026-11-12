// Command winpack builds both Windows desktop zips for one release version.
//
//	go run ./internal/desktop/winpack -version 1.2.3 -o bin
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("winpack", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "release version, major.minor.patch")
	outDir := flags.String("o", "bin", "directory for the zips")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	paths, err := Build(*version, *outDir, nil)
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintln(stdout, path)
	}
	return nil
}

// Command release uploads already-built installers for one verified version.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/LubyRuffy/eino-swarm/internal/release"
)

func main() {
	if err := publish(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func publish(args []string) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	version := flags.String("version", "", "release version, major.minor.patch")
	dir := flags.String("dir", "bin", "directory containing the zip and apk")
	check := flags.Bool("check", false, "validate the version and gh, then exit")
	platform := flags.String("platform", "", "installer platform: android or macos; empty requires both")
	target := flags.String("target", "", "verified full main source SHA")
	if err := flags.Parse(args); err != nil {
		return err
	}
	opts := release.Options{Version: *version, Dir: *dir, Platform: *platform, Target: *target}
	if *check {
		return release.Preflight(opts)
	}
	return release.Publish(opts)
}

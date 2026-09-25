// Command release uploads the Mac zip and the Android apk for one version.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/LubyRuffy/eino-swarm/internal/release"
)

func main() {
	version := flag.String("version", "", "release version, major.minor.patch")
	dir := flag.String("dir", "bin", "directory containing the zip and apk")
	check := flag.Bool("check", false, "validate the version and gh, then exit")
	flag.Parse()
	var err error
	if *check {
		err = release.Preflight(release.Options{Version: *version})
	} else {
		err = release.Publish(release.Options{Version: *version, Dir: *dir})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

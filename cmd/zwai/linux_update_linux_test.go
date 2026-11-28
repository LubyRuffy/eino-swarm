package main

import (
	"context"
	"net/http"
	"os"
	"runtime"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

const linuxUpdateTestFeed = "ZWAI_LINUX_UPDATE_TEST_FEED"

// The test-only binary drives the same boot and engine entry points while
// serving official release metadata and bytes from a private VM fixture.
func runLinuxArchiveUpdateFixture() bool {
	feedURL := os.Getenv(linuxUpdateTestFeed)
	if len(os.Args) < 2 || feedURL == "" {
		return false
	}
	version, linuxArchiveBuild = "1.0.0", "1"
	newLinuxUpdateFeed = func() *update.LinuxFeed {
		feed := &update.LinuxFeed{Current: version, Arch: runtime.GOARCH}
		feed.Fetch = func(ctx context.Context, rawURL string) (*http.Response, error) {
			parsed, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return nil, err
			}
			fixture, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL+parsed.URL.RequestURI(), nil)
			if err != nil {
				return nil, err
			}
			return http.DefaultClient.Do(fixture)
		}
		return feed
	}
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
	return true
}

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func linuxTestAsset(version, arch string, body []byte) releaseAsset {
	name := LinuxAssetName(version, arch)
	return releaseAsset{
		Name:   name,
		URL:    "https://github.com/" + ReleaseRepo + "/releases/download/v" + version + "/" + name,
		Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)),
	}
}

func linuxTestRelease(version string, assets ...releaseAsset) releaseDoc {
	return releaseDoc{TagName: "v" + version, Assets: assets}
}

func linuxTestJSON(t *testing.T, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return linuxTestResponse(body)
}

func linuxTestResponse(body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
}

func TestLinuxReleaseFeedSkipsAndroidOnlyAndFindsTheNewestCompatibleVerifiedArchive(t *testing.T) {
	body := []byte("release fixture")
	current := "1.2.2"
	arm := linuxTestAsset("1.10.1", "arm64", body)
	amd := linuxTestAsset("1.10.1", "amd64", body)
	badDigest := linuxTestAsset("1.11.1", "arm64", body)
	badDigest.Digest = ""
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			releases := []releaseDoc{
				linuxTestRelease("1.10.0", linuxTestAsset("1.10.0", arch, body)),
				linuxTestRelease("1.12.1", releaseAsset{Name: "zwai-1.12.1-android.apk"}),
				linuxTestRelease("1.11.1", badDigest),
				linuxTestRelease("1.10.1", arm, amd),
				linuxTestRelease("1.1.0", linuxTestAsset("1.1.0", arch, body)),
				{TagName: "v1.20.1", Prerelease: true, Assets: []releaseAsset{linuxTestAsset("1.20.1", arch, body)}},
				{TagName: "v1.20.2", Draft: true, Assets: []releaseAsset{linuxTestAsset("1.20.2", arch, body)}},
			}
			feed := LinuxFeed{
				Current: "v" + current, Arch: arch,
				Fetch: func(_ context.Context, source string) (*http.Response, error) {
					if source != releasesURL+"?per_page=100&page=1" {
						t.Fatalf("unexpected metadata URL: %s", source)
					}
					return linuxTestJSON(t, releases), nil
				},
			}
			got := feed.Check(t.Context(), false)
			if got.Status != "available" || got.Current != current || got.Offer == nil ||
				got.Offer.Version != "1.10.1" || got.Offer.AssetName != LinuxAssetName("1.10.1", arch) {
				t.Fatalf("checked release = %+v, offer = %+v", got, got.Offer)
			}
		})
	}
}

func TestLinuxReleaseFeedCachesStableResultsAndRefreshesOnDemand(t *testing.T) {
	now := time.Now()
	hits := 0
	feed := LinuxFeed{Current: "1.2.3", Arch: "amd64", now: func() time.Time { return now }}
	feed.Fetch = func(_ context.Context, _ string) (*http.Response, error) {
		hits++
		switch hits {
		case 1:
			return nil, errors.New("offline")
		case 3:
			return linuxTestJSON(t, []releaseDoc{}), nil
		default:
			return linuxTestJSON(t, []releaseDoc{linuxTestRelease("1.2.4", linuxTestAsset("1.2.4", "amd64", nil))}), nil
		}
	}
	if got := feed.Check(t.Context(), false); got.Status != "error" || got.Message != "offline" {
		t.Fatalf("offline result: %+v", got)
	}
	got := feed.Check(t.Context(), false)
	if got.Status != "available" || got.Offer == nil {
		t.Fatalf("after retry: %+v", got)
	}
	got.Offer.Version = "mutated"
	if hit := feed.Check(t.Context(), false); hit.Offer == nil || hit.Offer.Version != "1.2.4" || hits != 2 {
		t.Fatalf("cached response changed: %+v, requests = %d", hit.Offer, hits)
	}
	if fresh := feed.Check(t.Context(), true); fresh.Status != "current" || hits != 3 {
		t.Fatalf("force refresh: %+v, requests = %d", fresh, hits)
	}
	if cached := feed.Check(t.Context(), false); cached.Status != "current" || hits != 3 {
		t.Fatalf("cached current: %+v, requests = %d", cached, hits)
	}
	now = now.Add(checkTTL)
	if expired := feed.Check(t.Context(), false); expired.Status != "available" || hits != 4 {
		t.Fatalf("after expiration: %+v, requests = %d", expired, hits)
	}
}

func TestLinuxReleaseFeedBoundsPaginationAndRejectsMalformedMetadata(t *testing.T) {
	t.Run("later page", func(t *testing.T) {
		feed := LinuxFeed{Current: "1.2.3", Arch: "amd64"}
		feed.Fetch = func(_ context.Context, source string) (*http.Response, error) {
			switch source {
			case releasesURL + "?per_page=100&page=1":
				return linuxTestJSON(t, make([]releaseDoc, 100)), nil
			case releasesURL + "?per_page=100&page=2":
				return linuxTestJSON(t, []releaseDoc{linuxTestRelease("1.2.4", linuxTestAsset("1.2.4", "amd64", nil))}), nil
			default:
				t.Fatalf("unexpected metadata URL: %s", source)
				return nil, nil
			}
		}
		if got := feed.Check(t.Context(), false); got.Status != "available" || got.Offer.Version != "1.2.4" {
			t.Fatalf("later page: %+v", got)
		}
	})
	t.Run("page budget", func(t *testing.T) {
		hits := 0
		feed := LinuxFeed{Current: "1.2.3", Arch: "amd64", Fetch: func(_ context.Context, _ string) (*http.Response, error) {
			hits++
			return linuxTestJSON(t, make([]releaseDoc, 100)), nil
		}}
		if got := feed.Check(t.Context(), false); got.Status != "error" || !strings.Contains(got.Message, "limit") || hits != 10 {
			t.Fatalf("unbounded page walk: %+v, requests = %d", got, hits)
		}
	})
	for _, body := range []string{"not JSON", "null", "{}"} {
		t.Run(body, func(t *testing.T) {
			feed := LinuxFeed{Current: "1.2.3", Arch: "amd64", Fetch: func(_ context.Context, _ string) (*http.Response, error) {
				return linuxTestResponse([]byte(body)), nil
			}}
			if got := feed.Check(t.Context(), false); got.Status != "error" {
				t.Fatalf("malformed feed was accepted: %+v", got)
			}
		})
	}
	for _, tc := range []struct{ arch, version, status string }{
		{"i386", "1.2.3", "unsupported"}, {"amd64", "dev", "error"},
	} {
		feed := LinuxFeed{Current: tc.version, Arch: tc.arch}
		if got := feed.Check(t.Context(), false); got.Status != tc.status {
			t.Fatalf("%s %s = %+v", tc.arch, tc.version, got)
		}
	}
}

func TestLinuxReleaseAssetMustMatchVersionArchitectureOfficialURLAndDigest(t *testing.T) {
	const version = "1.2.4"
	work := linuxTestAsset(version, "amd64", nil)
	wrongURL := work
	wrongURL.URL = strings.ReplaceAll(work.URL, "/v1.2.4/", "/v1.2.5/")
	foreignURL := work
	foreignURL.URL = strings.ReplaceAll(work.URL, ReleaseRepo, "someone/else")
	badDigest := work
	badDigest.Digest = "sha256:" + strings.Repeat("z", 64)
	for name, assets := range map[string][]releaseAsset{
		"wrong version URL": {wrongURL},
		"wrong repository":  {foreignURL},
		"invalid digest":    {badDigest},
		"duplicates":        {work, work},
		"wrong arch":        {linuxTestAsset(version, "arm64", nil)},
	} {
		t.Run(name, func(t *testing.T) {
			if offer := pickLinuxAsset(version, "amd64", linuxTestRelease(version, assets...)); offer != nil {
				t.Fatalf("unsafe offer: %+v", offer)
			}
		})
	}
}

func TestLinuxStageRechecksTheExactConfirmedMetadataBeforeDownload(t *testing.T) {
	original := linuxTestAsset("1.2.4", "amd64", []byte("original"))
	changed := linuxTestAsset("1.2.4", "amd64", []byte("changed"))
	feed := LinuxFeed{Current: "1.2.3", Arch: "amd64", Fetch: func(_ context.Context, source string) (*http.Response, error) {
		switch source {
		case releasesURL + "?per_page=100&page=1":
			return linuxTestJSON(t, []releaseDoc{linuxTestRelease("1.2.4", original)}), nil
		case releasesURL + "/tags/v1.2.4":
			return linuxTestJSON(t, linuxTestRelease("1.2.4", changed)), nil
		default:
			t.Fatalf("unexpected request: %s", source)
			return nil, nil
		}
	}}
	if got := feed.Check(t.Context(), false); got.Status != "available" {
		t.Fatalf("initial release: %+v", got)
	}
	if _, err := feed.Stage(t.Context(), "1.2.4", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed digest error = %v", err)
	}
	for _, version := range []string{"", "1.2.2", "1.2.3", original.URL} {
		if _, err := feed.Stage(t.Context(), version, t.TempDir(), nil); err == nil {
			t.Fatalf("unconfirmed version %q passed", version)
		}
	}
}

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LinuxFeed selects official Linux archives. The installer supplies its own
// version and architecture so the paired phone never chooses a download URL.
type LinuxFeed struct {
	Current string
	Arch    string
	Fetch   func(context.Context, string) (*http.Response, error)

	verifyVersion func(context.Context, string, string) error
	now           func() time.Time
	mu            sync.Mutex
	checked       time.Time
	offer         *linuxOffer
}

type linuxOffer struct {
	Offer
	digest [sha256.Size]byte
}

// Check finds the latest stable release with a verified archive for this
// architecture. An Android-only release does not supersede a Linux package.
func (f *LinuxFeed) Check(ctx context.Context, fresh bool) Result {
	current := Canonical(f.Current)
	if linuxMachine(f.Arch) == 0 {
		return Result{Status: "unsupported", Current: f.Current}
	}
	if current == "" {
		return Result{Status: "error", Current: f.Current, Message: "this build has no release version"}
	}
	now := time.Now
	if f.now != nil {
		now = f.now
	}
	f.mu.Lock()
	age := now().Sub(f.checked)
	if !fresh && !f.checked.IsZero() && age >= 0 && age < checkTTL {
		result := linuxResult(current, f.offer)
		f.mu.Unlock()
		return result
	}
	f.mu.Unlock()

	offer, err := f.latest(ctx, current)
	if err != nil {
		return Result{Status: "error", Current: current, Message: err.Error()}
	}
	f.mu.Lock()
	f.checked, f.offer = now(), offer
	f.mu.Unlock()
	return linuxResult(current, offer)
}

func linuxResult(current string, offer *linuxOffer) Result {
	if offer == nil {
		return Result{Status: "current", Current: current}
	}
	copy := offer.Offer
	return Result{Status: "available", Current: current, Offer: &copy}
}

func (f *LinuxFeed) latest(ctx context.Context, current string) (*linuxOffer, error) {
	var best *linuxOffer
	for page := 1; page <= 10; page++ {
		raw, err := getReleaseJSON(ctx, fmt.Sprintf("%s?per_page=100&page=%d", releasesURL, page), f.Fetch)
		if err != nil {
			return nil, err
		}
		var releases []releaseDoc
		if err := json.Unmarshal(raw, &releases); err != nil || releases == nil {
			return nil, errors.New("the version response could not be read")
		}
		for _, row := range releases {
			version := Canonical(row.TagName)
			if !Newer(version, current) || (best != nil && !Newer(version, best.Version)) {
				continue
			}
			if offer := pickLinuxAsset(version, f.Arch, row); offer != nil {
				best = offer
			}
		}
		if len(releases) < 100 {
			return best, nil
		}
	}
	return nil, errors.New("the release list exceeded the update search limit")
}

func (f *LinuxFeed) resolve(ctx context.Context, version string) (*linuxOffer, error) {
	want := Canonical(version)
	if linuxMachine(f.Arch) == 0 || !Newer(want, f.Current) {
		return nil, errors.New("the requested Linux version must be newer than this build")
	}
	f.mu.Lock()
	knownOffer := f.offer
	f.mu.Unlock()
	raw, err := getReleaseJSON(ctx, releasesURL+"/tags/v"+want, f.Fetch)
	if err != nil {
		return nil, err
	}
	var row releaseDoc
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, errors.New("the version response could not be read")
	}
	offer := pickLinuxAsset(want, f.Arch, row)
	if offer == nil {
		return nil, errors.New("this release has no verified Linux archive for this architecture")
	}
	if knownOffer != nil && knownOffer.Version == want && knownOffer.digest != offer.digest {
		return nil, errors.New("the confirmed Linux release asset changed")
	}
	return offer, nil
}

func pickLinuxAsset(version, arch string, row releaseDoc) *linuxOffer {
	tag := "v" + version
	if row.TagName != tag || row.Prerelease || row.Draft {
		return nil
	}
	name := LinuxAssetName(version, arch)
	assetURL := "https://github.com/" + ReleaseRepo + "/releases/download/" + tag + "/" + name
	var offer *linuxOffer
	for _, asset := range row.Assets {
		if asset.Name != name {
			continue
		}
		digestHex, ok := strings.CutPrefix(asset.Digest, "sha256:")
		var digest [sha256.Size]byte
		if !ok || len(digestHex) != hex.EncodedLen(len(digest)) || asset.URL != assetURL || offer != nil {
			return nil
		}
		if _, err := hex.Decode(digest[:], []byte(digestHex)); err != nil {
			return nil
		}
		offer = &linuxOffer{Offer: Offer{
			Version: version, PageURL: AllowedPage(row.HTMLURL), AssetURL: assetURL, AssetName: name,
		}, digest: digest}
	}
	return offer
}

func linuxMachine(arch string) uint16 {
	switch arch {
	case "amd64":
		return 62
	case "arm64":
		return 183
	default:
		return 0
	}
}

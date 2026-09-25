package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
)

const checkTTL = 6 * time.Hour

// Offer is one newer Mac build on the public release.
type Offer struct {
	Version   string `json:"version"`
	PageURL   string `json:"page_url"`
	AssetURL  string `json:"asset_url"`
	AssetName string `json:"asset_name"`
}

// Result is what GET /api/update returns. Status is current, available,
// unsupported, or error. Message is for the person reading the menu.
type Result struct {
	Status  string `json:"status"`
	Current string `json:"current,omitempty"`
	Message string `json:"message,omitempty"`
	Offer   *Offer `json:"offer,omitempty"`
}

// Checker is the desktop update feed the HTTP server calls.
type Checker interface {
	Check(ctx context.Context, fresh bool) Result
	Apply(ctx context.Context, version string, desktopPIDs []int) error
}

// Service talks to GitHub and installs a downloaded zwai.app over this one.
type Service struct {
	Current string
	Arch    string
	OS      string
	Fetch   func(ctx context.Context, rawURL string) (*http.Response, error)
	Exe     func() (string, error)
	Open    func(app string) error
	Signal  func(pid int)
	Now     func() time.Time
	Spawn   func(script string) error

	mu    sync.Mutex
	cache *cached
}

type cached struct {
	at     time.Time
	result Result
}

func (s *Service) arch() string {
	if s.Arch != "" {
		return s.Arch
	}
	return HostArch()
}

func (s *Service) os() string {
	if s.OS != "" {
		return s.OS
	}
	return hostOS()
}

func hostOS() string {
	return system(runtime.GOOS, runtime.GOARCH)
}

func system(goos, goarch string) string {
	if archFor(goos, goarch) == "" {
		return "other"
	}
	return "darwin"
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Check reads the latest public release. A fresh check ignores the
// six-hour memory cache the automatic banner uses.
func (s *Service) Check(ctx context.Context, fresh bool) Result {
	current := Canonical(s.Current)
	if s.os() != "darwin" || s.arch() == "" {
		return Result{Status: "unsupported", Current: s.Current, Message: "desktop updates are published for macOS"}
	}
	if current == "" {
		return Result{Status: "error", Current: s.Current, Message: "this build has no release version"}
	}
	if !fresh {
		if hit, ok := s.freshCache(current); ok {
			return hit
		}
	}
	body, err := s.getJSON(ctx, LatestURL())
	if err != nil {
		return Result{Status: "error", Current: current, Message: err.Error()}
	}
	result := classify(current, s.arch(), body)
	s.store(current, result)
	return result
}

func (s *Service) freshCache(current string) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil || s.cache.result.Current != current {
		return Result{}, false
	}
	age := s.now().Sub(s.cache.at)
	if age < 0 || age >= checkTTL {
		return Result{}, false
	}
	return s.cache.result, true
}

func (s *Service) store(current string, result Result) {
	if result.Status == "error" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := result
	copied.Current = current
	s.cache = &cached{at: s.now(), result: copied}
}

func (s *Service) getJSON(ctx context.Context, rawURL string) ([]byte, error) {
	res, err := s.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		msg := githubMessage(raw)
		if msg == "" {
			msg = res.Status
		}
		return nil, errors.New(msg)
	}
	return raw, nil
}

func (s *Service) fetch(ctx context.Context, rawURL string) (*http.Response, error) {
	if s.Fetch != nil {
		return s.Fetch(ctx, rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "zwai")
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !AllowedHost(req.URL.Hostname()) {
				return errors.New("update download left GitHub")
			}
			return nil
		},
	}
	return client.Do(req)
}

func githubMessage(raw []byte) string {
	var row struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return ""
	}
	return strings.TrimSpace(row.Message)
}

func classify(current, arch string, body []byte) Result {
	var row releaseDoc
	if err := json.Unmarshal(body, &row); err != nil {
		return Result{Status: "error", Current: current, Message: "the version response could not be read"}
	}
	if row.Draft || row.Prerelease {
		return Result{Status: "current", Current: current}
	}
	tag := Canonical(row.TagName)
	if tag == "" {
		msg := strings.TrimSpace(row.Message)
		if msg == "" {
			msg = "the version response could not be read"
		}
		return Result{Status: "error", Current: current, Message: msg}
	}
	if !Newer(tag, current) {
		return Result{Status: "current", Current: current}
	}
	offer := pickAsset(tag, arch, row)
	if offer == nil {
		return Result{Status: "error", Current: current, Message: "this release has no macOS build"}
	}
	return Result{Status: "available", Current: current, Offer: offer}
}

type releaseDoc struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Message    string `json:"message"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func pickAsset(version, arch string, row releaseDoc) *Offer {
	want := DarwinAssetName(version, arch)
	page := AllowedPage(row.HTMLURL)
	var assetURL, assetName string
	for _, item := range row.Assets {
		if item.Name != want {
			continue
		}
		assetURL = AllowedDownload(item.URL)
		assetName = item.Name
		break
	}
	if assetURL == "" && page == "" {
		return nil
	}
	if assetURL == "" {
		return nil
	}
	return &Offer{Version: version, PageURL: page, AssetURL: assetURL, AssetName: assetName}
}

// AllowedDownload accepts only this repo's release files on github.com.
// The byte redirect is checked separately, so a public file on the asset
// host is not enough.
func AllowedDownload(raw string) string {
	u := https(raw)
	if u == nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	prefix := "/" + ReleaseRepo + "/releases/download/"
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	return u.String()
}

// AllowedPage is this repo's release page.
func AllowedPage(raw string) string {
	u := https(raw)
	if u == nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/"+ReleaseRepo+"/releases/") {
		return ""
	}
	return u.String()
}

// AllowedHost is github.com or a host GitHub redirects release bytes to.
func AllowedHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "api.github.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}

func https(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil
	}
	return u
}

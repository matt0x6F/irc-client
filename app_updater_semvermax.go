package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// semverMaxReleasePage is how many releases to scan when picking the newest
// prerelease. GitHub's list-releases endpoint is NOT ordered by semver — nor by
// any field we can influence (we confirmed rc.100 was returned mid-page despite
// being newest by tag, created_at, published_at, and internal release id). So we
// pull a generous page and select the maximum ourselves. 100 is GitHub's
// per-request maximum and comfortably covers any realistic prerelease train; a
// newer release GitHub happens to sort past position 100 would still be missed,
// which we accept over paginating the entire release history on every check.
const semverMaxReleasePage = 100

// semverMaxProvider is the updater.Provider backing the *prerelease* channel. It
// exists because the stock github.Provider takes the first non-draft entry from
// /releases and trusts that to be the newest — but GitHub's list order is
// internal and not version-sorted, so a freshly published prerelease is often
// returned mid-list. The stock provider then reports "You're Up to Date" while a
// newer build sits a few positions down (the exact rc.100-behind-rc.99 bug).
//
// This provider instead fetches a page and selects the highest release by SemVer
// 2.0.0 precedence (numeric prerelease identifiers compared numerically, so
// rc.100 > rc.99). Everything downstream is kept identical to the stock
// provider: it reuses our matchUpdateAsset for asset selection, attaches the
// SHA256SUMS digest for verification, and delegates the actual byte transfer to
// an embedded stock github.Provider (whose Download only needs the metadata we
// populate). The stable channel still uses the stock provider unchanged —
// /releases/latest is correctly semver-ranked by GitHub and excludes
// prereleases, so it never hit this bug.
type semverMaxProvider struct {
	repo         string
	base         string
	checksumName string
	matcher      github.AssetMatcher
	client       *http.Client
	dl           *github.Provider // embedded solely for its Download implementation
}

// newSemverMaxProvider builds the prerelease provider from the same github.Config
// the stock provider uses, so the two channels share repo, checksum asset, and
// asset-matching configuration. The embedded stock provider handles Download.
func newSemverMaxProvider(cfg github.Config) (*semverMaxProvider, error) {
	if strings.TrimSpace(cfg.Repository) == "" || !strings.Contains(cfg.Repository, "/") {
		return nil, errors.New("semvermax: Repository must be in \"owner/repo\" form")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	matcher := cfg.AssetMatcher
	if matcher == nil {
		matcher = github.DefaultAssetMatcher
	}
	dl, err := github.New(cfg)
	if err != nil {
		return nil, err
	}
	return &semverMaxProvider{
		repo:         cfg.Repository,
		base:         base,
		checksumName: cfg.ChecksumAsset,
		matcher:      matcher,
		client:       client,
		dl:           dl,
	}, nil
}

// Name is fixed to "github" so the Updater routes a follow-up Download back
// through the channelRoutingProvider (which forwards to this provider's
// Download) exactly as it does for the stock provider.
func (p *semverMaxProvider) Name() string { return "github" }

// Check fetches a page of releases, selects the highest by semver, and — when it
// is newer than the running version — resolves the platform asset and its
// checksum. Returns (nil, nil) when up to date, matching the Provider contract.
func (p *semverMaxProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	rels, err := p.fetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	best := selectLatestRelease(rels)
	if best == nil {
		return nil, nil
	}
	if !isNewerTag(best.TagName, req.CurrentVersion) {
		return nil, nil // up to date
	}

	assets := make([]github.ReleaseAsset, len(best.Assets))
	for i, a := range best.Assets {
		assets[i] = github.ReleaseAsset{
			Name:        a.Name,
			ContentType: a.ContentType,
			Size:        a.Size,
			URL:         a.BrowserDownloadURL,
		}
	}
	idx := p.matcher(req, assets)
	if idx < 0 || idx >= len(best.Assets) {
		return nil, fmt.Errorf("semvermax: release %s has no asset for %s/%s", best.TagName, req.Platform, req.Arch)
	}
	picked := best.Assets[idx]

	out := &updater.Release{
		Version:     trimVPrefix(best.TagName),
		Channel:     "prerelease",
		Name:        best.Name,
		Notes:       best.Body,
		PublishedAt: best.PublishedAt,
		Artifact: updater.Artifact{
			Filename: picked.Name,
			Filetype: fileExtOf(picked.Name),
			Size:     picked.Size,
			Platform: req.Platform,
			Arch:     req.Arch,
		},
		// These keys mirror the stock provider so the embedded provider's
		// Download (which reads github.asset.url) works unchanged.
		Metadata: map[string]any{
			"github.asset.id":          picked.ID,
			"github.asset.contentType": picked.ContentType,
			"github.asset.url":         picked.BrowserDownloadURL,
			"github.release.tag":       best.TagName,
			"github.release.htmlURL":   best.HTMLURL,
		},
	}

	if p.checksumName != "" {
		digest, err := p.fetchChecksum(ctx, best.Assets, picked.Name)
		if err != nil {
			return nil, fmt.Errorf("semvermax: load checksum sidecar: %w", err)
		}
		if digest != nil {
			out.Verification = &updater.Verification{DigestAlgo: "sha256", Digest: digest}
		}
	}
	return out, nil
}

// Download delegates to the embedded stock provider, which streams the asset
// named by Metadata["github.asset.url"] with the framework's redirect/auth
// handling. Check populates that key, so no reimplementation is needed here.
func (p *semverMaxProvider) Download(ctx context.Context, r *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	return p.dl.Download(ctx, r, dst, onProgress)
}

// fetchReleases GETs /repos/{repo}/releases?per_page=N. A 404 (no releases yet)
// is treated as an empty list, not an error, matching the stock provider.
func (p *semverMaxProvider) fetchReleases(ctx context.Context) ([]ghRelease, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", p.base, p.repo, semverMaxReleasePage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("semvermax: api request: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("semvermax: api %d: %s", resp.StatusCode, body)
	}
	var list []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("semvermax: decode releases: %w", err)
	}
	return list, nil
}

// fetchChecksum downloads the SHA256SUMS sidecar and returns the raw digest for
// targetName, or nil when the sidecar or entry is absent. A malformed digest is
// an error, not a silent skip — verification must fail closed.
func (p *semverMaxProvider) fetchChecksum(ctx context.Context, assets []ghAsset, targetName string) ([]byte, error) {
	url := ""
	for _, a := range assets {
		if a.Name == p.checksumName {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("checksum sidecar HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseChecksumFor(string(body), targetName)
}

// selectLatestRelease returns the published (non-draft) release with the highest
// SemVer 2.0.0 precedence, or nil when the list contains none. This is the fix:
// selection by version rather than by GitHub's list position. Numeric prerelease
// identifiers compare numerically here, so v…-rc.100 outranks v…-rc.99.
func selectLatestRelease(rels []ghRelease) *ghRelease {
	var best *ghRelease
	for i := range rels {
		if rels[i].Draft {
			continue
		}
		if best == nil || semver.Compare(canonicalTag(rels[i].TagName), canonicalTag(best.TagName)) > 0 {
			best = &rels[i]
		}
	}
	return best
}

// parseChecksumFor extracts the digest for target from a `sha256sum`-style
// listing ("<hex>  <name>"), tolerating the "*" binary-mode marker and a "./"
// prefix. Returns nil when target is absent; errors on a malformed hex digest.
func parseChecksumFor(body, target string) ([]byte, error) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		name = strings.TrimPrefix(name, "./")
		if name != target {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil {
			return nil, fmt.Errorf("malformed digest for %s: %w", target, err)
		}
		return digest, nil
	}
	return nil, nil
}

// trimVPrefix strips a leading "v"/"V" from a release tag.
func trimVPrefix(tag string) string {
	if len(tag) > 0 && (tag[0] == 'v' || tag[0] == 'V') {
		return tag[1:]
	}
	return tag
}

// canonicalTag renders a tag in the single-leading-"v" form golang.org/x/mod/semver
// expects. Invalid semver sorts below every valid version (x/mod/semver's
// behaviour), so a malformed tag simply never wins selection.
func canonicalTag(tag string) string {
	t := trimVPrefix(tag)
	if t == "" {
		return ""
	}
	return "v" + t
}

// isNewerTag reports whether tag is strictly newer than current under SemVer
// precedence. Empty tag is never newer; a non-empty tag beats an empty current.
func isNewerTag(tag, current string) bool {
	if trimVPrefix(tag) == "" {
		return false
	}
	if trimVPrefix(current) == "" {
		return true
	}
	return semver.Compare(canonicalTag(tag), canonicalTag(current)) > 0
}

// fileExtOf returns the lowercased extension (without the dot), or "".
func fileExtOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i+1:])
	}
	return ""
}

// ghRelease / ghAsset are the subset of the GitHub Releases API this provider
// decodes. They mirror the stock provider's internal shapes.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	ContentType        string `json:"content_type"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

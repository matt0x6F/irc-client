package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// TestSelectLatestRelease pins the core of the prerelease fix: pick the highest
// SemVer, regardless of list position, and never pick a draft. The rc.100 case
// is the real bug — GitHub returned it mid-list, below rc.99, so the stock
// "take position 0" provider missed it. rc.9 vs rc.10 guards numeric (not
// lexicographic) ordering of prerelease identifiers.
func TestSelectLatestRelease(t *testing.T) {
	cases := []struct {
		name string
		in   []ghRelease
		want string // expected TagName, "" for nil
	}{
		{
			name: "highest semver wins even when GitHub lists it mid-page",
			in: []ghRelease{
				{TagName: "v26.6.8-rc.99"},
				{TagName: "v26.6.8-rc.98"},
				{TagName: "v26.6.8-rc.100"}, // the actual newest, buried by GitHub
				{TagName: "v26.6.8-rc.95"},
			},
			want: "v26.6.8-rc.100",
		},
		{
			name: "numeric prerelease ordering: rc.10 > rc.9",
			in:   []ghRelease{{TagName: "v1.0.0-rc.9"}, {TagName: "v1.0.0-rc.10"}},
			want: "v1.0.0-rc.10",
		},
		{
			name: "drafts are skipped even when highest",
			in: []ghRelease{
				{TagName: "v2.0.0-rc.5"},
				{TagName: "v2.0.0-rc.9", Draft: true}, // unpublished; must not win
			},
			want: "v2.0.0-rc.5",
		},
		{
			name: "empty list yields nil",
			in:   nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectLatestRelease(tc.in)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("want nil, got %q", got.TagName)
				}
				return
			}
			if got == nil || got.TagName != tc.want {
				t.Fatalf("want %q, got %v", tc.want, got)
			}
		})
	}
}

// TestSemverMaxProviderCheck exercises the full Check path against a canned
// GitHub API whose /releases ordering mirrors the observed bug: rc.100 is
// returned in the middle of the page, below rc.99. The stock provider would
// pick rc.99 and report "up to date" for a client on rc.99; ours must offer
// rc.100 and attach the SHA256SUMS digest for verification.
func TestSemverMaxProviderCheck(t *testing.T) {
	const digestHex = "abababababababababababababababababababababababababababababababab"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			asset := func() []ghAsset {
				return []ghAsset{
					{Name: "cascade-universal.zip", Size: 42, BrowserDownloadURL: base + "/app.zip"},
					{Name: "SHA256SUMS", BrowserDownloadURL: base + "/SHA256SUMS"},
				}
			}
			// Deliberately NOT in semver order — rc.100 sits below rc.99.
			list := []ghRelease{
				{TagName: "v26.6.8-rc.99", Prerelease: true, Assets: asset()},
				{TagName: "v26.6.8-rc.98", Prerelease: true, Assets: asset()},
				{TagName: "v26.6.8-rc.100", Prerelease: true, Name: "rc100", Body: "notes", Assets: asset()},
				{TagName: "v26.6.8-rc.95", Prerelease: true, Assets: asset()},
			}
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte(digestHex + "  cascade-universal.zip\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p, err := newSemverMaxProvider(github.Config{
		Repository:    "matt0x6f/irc-client",
		BaseURL:       srv.URL,
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  matchUpdateAsset,
		HTTPClient:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("newSemverMaxProvider: %v", err)
	}

	req := updater.CheckRequest{CurrentVersion: "26.6.8-rc.99", Platform: "darwin", Arch: "arm64"}
	rel, err := p.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil {
		t.Fatal("Check returned nil; expected rc.100 to be offered over rc.99")
	}
	if rel.Version != "26.6.8-rc.100" {
		t.Fatalf("offered version %q, want 26.6.8-rc.100", rel.Version)
	}
	if rel.Verification == nil || rel.Verification.DigestAlgo != "sha256" {
		t.Fatalf("expected sha256 verification, got %+v", rel.Verification)
	}
	if url, _ := rel.Metadata["github.asset.url"].(string); !strings.HasSuffix(url, "/app.zip") {
		t.Fatalf("download URL not wired for stock-provider delegation: %q", url)
	}

	// A client already on the highest version must see "up to date" (nil).
	up := updater.CheckRequest{CurrentVersion: "26.6.8-rc.100", Platform: "darwin", Arch: "arm64"}
	rel, err = p.Check(context.Background(), up)
	if err != nil {
		t.Fatalf("Check (up to date): %v", err)
	}
	if rel != nil {
		t.Fatalf("expected nil (up to date), got %q", rel.Version)
	}
}

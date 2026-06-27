package unfurl

import (
	"strings"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	html := `<html><head>
		<title>Fallback Title</title>
		<meta property="og:title" content="OG Title">
		<meta property="og:description" content="OG Desc">
		<meta property="og:site_name" content="Example">
		<meta property="og:image" content="https://cdn.example/x.png">
	</head><body>ignored</body></html>`

	md := parseMetadata(strings.NewReader(html))
	if md.Title != "OG Title" {
		t.Errorf("Title = %q, want %q", md.Title, "OG Title")
	}
	if md.Description != "OG Desc" {
		t.Errorf("Description = %q", md.Description)
	}
	if md.SiteName != "Example" {
		t.Errorf("SiteName = %q", md.SiteName)
	}
	if md.ImageURL != "https://cdn.example/x.png" {
		t.Errorf("ImageURL = %q", md.ImageURL)
	}
}

func TestParseMetadataTitleFallback(t *testing.T) {
	md := parseMetadata(strings.NewReader(`<html><head><title>Just Title</title></head></html>`))
	if md.Title != "Just Title" {
		t.Errorf("Title = %q, want %q", md.Title, "Just Title")
	}
}

func TestParseMetadataEmpty(t *testing.T) {
	md := parseMetadata(strings.NewReader(`<html><body>no head</body></html>`))
	if md.Title != "" || md.ImageURL != "" {
		t.Errorf("expected empty metadata, got %+v", md)
	}
}

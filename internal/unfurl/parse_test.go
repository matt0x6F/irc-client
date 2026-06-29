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

func TestParseMetadataTwitterFallback(t *testing.T) {
	html := `<html><head>
		<meta name="twitter:title" content="Twitter Title">
		<meta name="twitter:description" content="Twitter Desc">
		<meta name="twitter:image" content="https://cdn.example/twitter.png">
	</head></html>`

	md := parseMetadata(strings.NewReader(html))
	if md.Title != "Twitter Title" {
		t.Errorf("Title = %q, want %q", md.Title, "Twitter Title")
	}
	if md.Description != "Twitter Desc" {
		t.Errorf("Description = %q, want %q", md.Description, "Twitter Desc")
	}
	if md.ImageURL != "https://cdn.example/twitter.png" {
		t.Errorf("ImageURL = %q, want %q", md.ImageURL, "https://cdn.example/twitter.png")
	}
}

func TestParseMetadataOpenGraphBeatsTwitter(t *testing.T) {
	// Test with OG first, then Twitter
	html1 := `<html><head>
		<meta property="og:title" content="OG Title">
		<meta property="og:image" content="https://cdn.example/og.png">
		<meta name="twitter:title" content="Twitter Title">
		<meta name="twitter:image" content="https://cdn.example/twitter.png">
	</head></html>`

	md1 := parseMetadata(strings.NewReader(html1))
	if md1.Title != "OG Title" {
		t.Errorf("Title (OG first) = %q, want %q", md1.Title, "OG Title")
	}
	if md1.ImageURL != "https://cdn.example/og.png" {
		t.Errorf("ImageURL (OG first) = %q, want %q", md1.ImageURL, "https://cdn.example/og.png")
	}

	// Test with Twitter first, then OG (harder case)
	html2 := `<html><head>
		<meta name="twitter:title" content="Twitter Title">
		<meta name="twitter:image" content="https://cdn.example/twitter.png">
		<meta property="og:title" content="OG Title">
		<meta property="og:image" content="https://cdn.example/og.png">
	</head></html>`

	md2 := parseMetadata(strings.NewReader(html2))
	if md2.Title != "OG Title" {
		t.Errorf("Title (Twitter first) = %q, want %q", md2.Title, "OG Title")
	}
	if md2.ImageURL != "https://cdn.example/og.png" {
		t.Errorf("ImageURL (Twitter first) = %q, want %q", md2.ImageURL, "https://cdn.example/og.png")
	}
}

func TestParseMetadataCollapsesWhitespace(t *testing.T) {
	// Real-world og:description values frequently contain newlines and runs of
	// spaces that render as jammed/ragged text in a clamped card. Collapse them.
	html := `<html><head>
		<meta property="og:title" content="Spaced   Title">
		<meta property="og:description" content="line one
line two		tabbed   spaced">
	</head></html>`

	md := parseMetadata(strings.NewReader(html))
	if md.Title != "Spaced Title" {
		t.Errorf("Title = %q, want %q", md.Title, "Spaced Title")
	}
	if md.Description != "line one line two tabbed spaced" {
		t.Errorf("Description = %q, want collapsed single-spaced text", md.Description)
	}
}

func TestParseMetadataNameDescription(t *testing.T) {
	html := `<html><head>
		<meta name="description" content="Generic description">
	</head></html>`

	md := parseMetadata(strings.NewReader(html))
	if md.Description != "Generic description" {
		t.Errorf("Description = %q, want %q", md.Description, "Generic description")
	}
}

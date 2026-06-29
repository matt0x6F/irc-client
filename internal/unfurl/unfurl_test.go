package unfurl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func permissiveGuard(_ netip.Addr) error { return nil }

func TestFetchWithHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<head>
			<meta property="og:title" content="Hello">
			<meta property="og:description" content="World">
			<meta property="og:image" content="/img.png"></head>`))
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // full 8-byte PNG signature
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := fetchWith(context.Background(), srv.URL+"/page", permissiveGuard)
	if err != nil {
		t.Fatalf("fetchWith: %v", err)
	}
	if p.Title != "Hello" || p.Description != "World" {
		t.Errorf("got title=%q desc=%q", p.Title, p.Description)
	}
	if !strings.HasPrefix(p.ImageDataURI, "data:image/png;base64,") {
		t.Errorf("image not embedded as data URI: %q", p.ImageDataURI)
	}
}

func TestFetchWithNonHTMLRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"x":1}`))
	}))
	defer srv.Close()
	if _, err := fetchWith(context.Background(), srv.URL, permissiveGuard); err == nil {
		t.Fatal("expected error for non-JSON/HTML/image content type")
	}
}

// TestFetchWithDirectImageURL verifies that a URL which resolves directly to an
// image (e.g. a pasted https://host/upload/x.png link) yields an image-only
// preview card rather than the "unexpected content-type" error that the
// HTML-only path used to produce. The mime type must come from the payload
// bytes (DetectContentType), not the server header.
func TestFetchWithDirectImageURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Full 8-byte PNG signature so DetectContentType reports image/png.
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	}))
	defer srv.Close()

	p, err := fetchWith(context.Background(), srv.URL+"/uploads/photo.png", permissiveGuard)
	if err != nil {
		t.Fatalf("fetchWith: unexpected error: %v", err)
	}
	if !strings.HasPrefix(p.ImageDataURI, "data:image/png;base64,") {
		t.Errorf("image not embedded as data URI: %q", p.ImageDataURI)
	}
	if p.Title != "photo.png" {
		t.Errorf("Title = %q, want the image filename %q", p.Title, "photo.png")
	}
}

// TestFetchWithImageURLSpoofedType verifies that a server claiming image/* but
// serving non-image bytes is rejected (the data URI mime is decided by the
// payload, never the server header).
func TestFetchWithImageURLSpoofedType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	if _, err := fetchWith(context.Background(), srv.URL+"/x.png", permissiveGuard); err == nil {
		t.Fatal("expected error: payload is not actually an image")
	}
}

func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	if _, err := Fetch(context.Background(), "ftp://example.com/x"); err == nil {
		t.Fatal("expected scheme rejection")
	}
}

// TestFetchWithNonImagePayloadFallsBackToTextOnly verifies that a server lying
// about Content-Type (claiming image/png but serving HTML) cannot inject an
// arbitrary mime type into the data URI. The image failure must be non-fatal:
// the preview still returns title/description with an empty ImageDataURI.
func TestFetchWithNonImagePayloadFallsBackToTextOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<head>
			<meta property="og:title" content="Secure Title">
			<meta property="og:description" content="Safe Description">
			<meta property="og:image" content="/fake.png"></head>`))
	})
	mux.HandleFunc("/fake.png", func(w http.ResponseWriter, r *http.Request) {
		// Server claims image/png but sends HTML — DetectContentType will see text/html.
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("<html>not an image</html>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := fetchWith(context.Background(), srv.URL+"/page", permissiveGuard)
	if err != nil {
		t.Fatalf("fetchWith: unexpected error: %v", err)
	}
	if p.Title != "Secure Title" || p.Description != "Safe Description" {
		t.Errorf("got title=%q desc=%q", p.Title, p.Description)
	}
	if p.ImageDataURI != "" {
		t.Errorf("expected empty ImageDataURI (text-only preview), got %q", p.ImageDataURI)
	}
}

// TestFetchImageRejectsNonHTTPScheme verifies that dangerous og:image schemes
// (file://, ftp://, data:, etc.) are explicitly rejected at the fetchImage level,
// yielding a text-only preview. This is defense-in-depth: even if an attacker-
// controlled og:image contains a dangerous scheme, fetchImage rejects it.
func TestFetchImageRejectsNonHTTPScheme(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<head>
			<meta property="og:title" content="Page Title">
			<meta property="og:description" content="Page Description">
			<meta property="og:image" content="file:///etc/passwd"></head>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := fetchWith(context.Background(), srv.URL+"/page", permissiveGuard)
	if err != nil {
		t.Fatalf("fetchWith: unexpected error: %v", err)
	}
	if p.Title != "Page Title" || p.Description != "Page Description" {
		t.Errorf("got title=%q desc=%q", p.Title, p.Description)
	}
	if p.ImageDataURI != "" {
		t.Errorf("expected empty ImageDataURI (blocked scheme), got %q", p.ImageDataURI)
	}
}

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
		w.Write([]byte{0x89, 0x50, 0x4e, 0x47}) // PNG magic; bytes are enough for the test
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
		t.Fatal("expected error for non-HTML content type")
	}
}

func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	if _, err := Fetch(context.Background(), "ftp://example.com/x"); err == nil {
		t.Fatal("expected scheme rejection")
	}
}

package unfurl

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	fetchTimeout = 5 * time.Second
	maxRedirects = 3
	maxHTMLBytes = 1 << 20 // 1 MiB
	maxImgBytes  = 2 << 20 // 2 MiB
)

// LinkPreview holds the extracted preview data for a URL. Status is left empty
// here; the App layer stamps "ok"/"blocked"/"error" when persisting.
type LinkPreview struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	SiteName     string `json:"siteName"`
	ImageDataURI string `json:"imageDataUri"`
	FetchedAt    string `json:"fetchedAt"`
}

// Fetch retrieves preview metadata for rawURL using the production SSRF guard.
func Fetch(ctx context.Context, rawURL string) (*LinkPreview, error) {
	return fetchWith(ctx, rawURL, defaultIPGuard)
}

func fetchWith(ctx context.Context, rawURL string, guard ipGuard) (*LinkPreview, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("unfurl: bad url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q", ErrBlocked, u.Scheme)
	}

	client := newGuardedClient(fetchTimeout, maxRedirects, guard)

	body, finalURL, err := fetchBounded(ctx, client, rawURL, "text/html", maxHTMLBytes)
	if err != nil {
		return nil, err
	}
	md := parseMetadata(strings.NewReader(string(body)))

	preview := &LinkPreview{
		URL:         finalURL,
		Title:       md.Title,
		Description: md.Description,
		SiteName:    md.SiteName,
	}

	if md.ImageURL != "" {
		if imgURL := resolveURL(finalURL, md.ImageURL); imgURL != "" {
			if dataURI, err := fetchImage(ctx, client, imgURL); err == nil {
				preview.ImageDataURI = dataURI
			}
			// A failed/blocked image is non-fatal: keep the text-only card.
		}
	}
	return preview, nil
}

// fetchBounded GETs rawURL, enforces the content-type prefix, and reads at most
// maxBytes. It returns the body and the final (post-redirect) URL.
func fetchBounded(ctx context.Context, c *http.Client, rawURL, wantType string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Cascade-Chat-LinkPreview/1.0")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("DNT", "1")

	resp, err := c.Do(req)
	if err != nil {
		return nil, "", err // wraps ErrBlocked when the dialer rejected the IP
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unfurl: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), wantType) {
		return nil, "", fmt.Errorf("unfurl: unexpected content-type %q", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", err
	}
	return body, resp.Request.URL.String(), nil
}

func fetchImage(ctx context.Context, c *http.Client, imgURL string) (string, error) {
	// fetchBounded enforces status==200 and server Content-Type prefix "image/".
	body, _, err := fetchBounded(ctx, c, imgURL, "image/", maxImgBytes)
	if err != nil {
		return "", err
	}

	// DetectContentType is the sole authority for the data-URI mime type.
	// We never echo the server-declared Content-Type into the URI, so a lying
	// server cannot inject an arbitrary mime type.
	mime := http.DetectContentType(body)
	if !strings.HasPrefix(mime, "image/") {
		return "", fmt.Errorf("unfurl: payload is not an image (%s)", mime)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body), nil
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

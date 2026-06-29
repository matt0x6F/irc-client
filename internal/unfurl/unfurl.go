package unfurl

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

	body, contentType, finalURL, err := fetchTopLevel(ctx, client, rawURL)
	if err != nil {
		return nil, err
	}

	// A URL that resolves directly to an image (a pasted CDN/upload link) gets an
	// image-only card; everything else is parsed as an HTML document.
	if strings.HasPrefix(contentType, "image/") {
		return imagePreview(body, finalURL)
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

// imagePreview builds an image-only card for a URL that is itself an image. The
// mime type is decided solely by DetectContentType on the payload bytes (never
// the server-declared header), matching fetchImage's anti-spoofing guarantee.
func imagePreview(body []byte, finalURL string) (*LinkPreview, error) {
	mime := http.DetectContentType(body)
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("unfurl: payload is not an image (%s)", mime)
	}
	preview := &LinkPreview{
		URL:          finalURL,
		ImageDataURI: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body),
	}
	// Surface the host and file name so the card has a label beside the image.
	if u, err := url.Parse(finalURL); err == nil {
		preview.SiteName = u.Host
		if name := path.Base(u.Path); name != "" && name != "/" && name != "." {
			preview.Title = name
		}
	}
	return preview, nil
}

// newPreviewRequest builds a GET carrying the link-preview fetch headers.
func newPreviewRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cascade-Chat-LinkPreview/1.0")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("DNT", "1")
	return req, nil
}

// fetchTopLevel GETs rawURL and dispatches on the response Content-Type: HTML
// and image responses are both accepted (each with its own byte cap) so a URL
// that points straight at an image still produces a preview. Anything else is
// rejected. It returns the body, the lowercased Content-Type, and the final
// (post-redirect) URL.
func fetchTopLevel(ctx context.Context, c *http.Client, rawURL string) ([]byte, string, string, error) {
	req, err := newPreviewRequest(ctx, rawURL)
	if err != nil {
		return nil, "", "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, "", "", err // wraps ErrBlocked when the dialer rejected the IP
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("unfurl: status %d", resp.StatusCode)
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	var maxBytes int64
	switch {
	case strings.HasPrefix(ct, "text/html"):
		maxBytes = maxHTMLBytes
	case strings.HasPrefix(ct, "image/"):
		maxBytes = maxImgBytes
	default:
		return nil, "", "", fmt.Errorf("unfurl: unsupported content-type %q", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", "", err
	}
	return body, ct, resp.Request.URL.String(), nil
}

// fetchBounded GETs rawURL, enforces the content-type prefix, and reads at most
// maxBytes. It returns the body and the final (post-redirect) URL.
func fetchBounded(ctx context.Context, c *http.Client, rawURL, wantType string, maxBytes int64) ([]byte, string, error) {
	req, err := newPreviewRequest(ctx, rawURL)
	if err != nil {
		return nil, "", err
	}

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
	// Explicit http(s) scheme allowlist: reject file://, ftp://, data:, and other dangerous schemes.
	u, err := url.Parse(imgURL)
	if err != nil {
		return "", fmt.Errorf("unfurl: bad image url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: image scheme %q", ErrBlocked, u.Scheme)
	}

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

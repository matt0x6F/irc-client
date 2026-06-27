package unfurl

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

type metadata struct {
	Title       string
	Description string
	SiteName    string
	ImageURL    string
}

// parseMetadata extracts preview fields from an HTML document. It reads meta and
// title tags and stops at </head>, so it never walks the full body. OpenGraph
// wins; Twitter-card and <title> are fallbacks only when OG is absent.
func parseMetadata(r io.Reader) metadata {
	var md metadata
	var twTitle, twDesc, twImage, htmlTitle string

	z := html.NewTokenizer(r)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finalize(md, twTitle, twDesc, twImage, htmlTitle)

		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "head" {
				return finalize(md, twTitle, twDesc, twImage, htmlTitle)
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				if z.Next() == html.TextToken {
					htmlTitle = strings.TrimSpace(string(z.Text()))
				}
			case "meta":
				if !hasAttr {
					continue
				}
				prop, content := metaAttrs(z)
				switch prop {
				case "og:title":
					md.Title = content
				case "og:description":
					md.Description = content
				case "og:site_name":
					md.SiteName = content
				case "og:image", "og:image:url":
					md.ImageURL = content
				case "twitter:title":
					twTitle = content
				case "twitter:description":
					twDesc = content
				case "twitter:image":
					twImage = content
				case "description":
					if md.Description == "" {
						md.Description = content
					}
				}
			}
		}
	}
}

// metaAttrs returns the identifying key (property or name) and the content value
// of a <meta> tag.
func metaAttrs(z *html.Tokenizer) (key, content string) {
	for {
		k, v, more := z.TagAttr()
		switch string(k) {
		case "property", "name":
			key = strings.ToLower(strings.TrimSpace(string(v)))
		case "content":
			content = strings.TrimSpace(string(v))
		}
		if !more {
			return key, content
		}
	}
}

func finalize(md metadata, twTitle, twDesc, twImage, htmlTitle string) metadata {
	if md.Title == "" {
		md.Title = firstNonEmpty(twTitle, htmlTitle)
	}
	if md.Description == "" {
		md.Description = twDesc
	}
	if md.ImageURL == "" {
		md.ImageURL = twImage
	}
	return md
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

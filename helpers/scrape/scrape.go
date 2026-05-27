package scrape

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ParseHTML parses an HTML byte slice into a goquery document.
//
// The function is the consolidation point for half a dozen near-identical
// `goquery.NewDocumentFromReader(bytes.NewReader(body))` call sites that
// used to live in vench, avoventes, lawyer and a few enrichers. The
// error message is deliberately compact and prefixed "scrape: parse"
// so callers can keep wrapping with their own context
// (`fmt.Errorf("vench.GetDetail: %w", err)`).
func ParseHTML(body []byte) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("scrape: parse HTML: %w", err)
	}
	return doc, nil
}

// AbsoluteURL turns a relative href into an absolute URL given a base.
//
// The accepted shapes mirror the conventions every site adapter has
// independently re-derived:
//
//   - "https://example.fr/foo"   → returned verbatim
//   - "http://example.fr/foo"    → returned verbatim
//   - "/foo"                     → base + "/foo" (root-relative)
//   - "./foo"                    → base + "/foo" (current-page-relative
//     collapsed to root-relative; site-specific path arithmetic should
//     be done by the caller)
//   - "foo"                      → base + "/foo" (bare path)
//   - ""                         → "" (empty in, empty out)
//
// The base is used as-is without trailing-slash normalisation: a base of
// "https://www.example.fr" composes "https://www.example.fr/foo"; a
// base already ending with "/" would produce
// "https://www.example.fr//foo" — callers should pass a base WITHOUT a
// trailing slash. The helper deliberately does not call net/url.Parse
// to keep error semantics simple (no parse-error surface to wrap).
func AbsoluteURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		return ref
	case strings.HasPrefix(ref, "./"):
		return base + "/" + strings.TrimPrefix(ref, "./")
	case strings.HasPrefix(ref, "/"):
		return base + ref
	default:
		return base + "/" + ref
	}
}

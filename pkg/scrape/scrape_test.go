package scrape

import (
	"strings"
	"testing"
)

func TestParseHTML(t *testing.T) {
	doc, err := ParseHTML([]byte(`<html><body><h1>hello</h1></body></html>`))
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if got := strings.TrimSpace(doc.Find("h1").Text()); got != "hello" {
		t.Fatalf("h1 = %q, want hello", got)
	}
}

func TestParseHTML_EmptyBody(t *testing.T) {
	// goquery happily parses empty input; ensure we don't error out.
	_, err := ParseHTML(nil)
	if err != nil {
		t.Fatalf("ParseHTML(nil): %v", err)
	}
}

func TestAbsoluteURL(t *testing.T) {
	cases := []struct {
		name, base, ref, want string
	}{
		{"empty", "https://x.fr", "", ""},
		{"absolute_https", "https://x.fr", "https://other.fr/y", "https://other.fr/y"},
		{"absolute_http", "https://x.fr", "http://other.fr/y", "http://other.fr/y"},
		{"root_relative", "https://x.fr", "/foo", "https://x.fr/foo"},
		{"dot_relative", "https://x.fr", "./vente-1.html", "https://x.fr/vente-1.html"},
		{"bare_path", "https://x.fr", "vente-1.html", "https://x.fr/vente-1.html"},
		{"trim_whitespace", "https://x.fr", "  /foo  ", "https://x.fr/foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AbsoluteURL(tc.base, tc.ref)
			if got != tc.want {
				t.Errorf("AbsoluteURL(%q, %q) = %q, want %q", tc.base, tc.ref, got, tc.want)
			}
		})
	}
}

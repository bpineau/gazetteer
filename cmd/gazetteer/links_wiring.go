package main

import (
	"fmt"

	"github.com/bpineau/gazetteer/sources/links"
)

// The links source builds deep-link URLs purely from the listing's address /
// coordinates. Its CLI catalog descriptor and renderer are registered here, in
// one place, into the package-level maps defined in catalog.go / render.go.
func init() {
	sourceDescriptors[links.Name] = sourceDescriptor{
		Summary:  "Deep links to third-party tools (maps, prices/DVF, Géorisques, PLU, commune fiche) for the address — built from coordinates / INSEE / address, no HTTP.",
		Inputs:   []string{"lat/lon (or INSEE, or address)"},
		Coverage: "national",
	}
	sourceRenderers[links.Name] = renderLinks
}

// renderLinks summarises the deep links: a count headline + one detail line per
// link, in source order.
func renderLinks(data any) (string, []string) {
	r, ok := data.(*links.Result)
	if !ok || r == nil || r.IsEmpty() {
		return "no links", nil
	}
	extra := make([]string, 0, len(r.Links))
	for _, l := range r.Links {
		extra = append(extra, fmt.Sprintf("%s: %s", l.Label, l.URL))
	}
	return fmt.Sprintf("%d links", len(r.Links)), extra
}

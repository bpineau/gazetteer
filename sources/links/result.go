// Package links surfaces deep-link URLs to useful third-party tools and
// datasets for the listing's address.
//
// Unlike every other source it performs no HTTP and ships no dataset: it
// merely *builds* well-known deep links from the listing's coordinates and
// address fields (Lat/Lon, INSEE, city, address). The links point at maps and
// aerial imagery, price/transaction explorers, hazard reports, urbanism (PLU)
// viewers and commune fact-sheets — including tools whose data other sources
// already extract, so a human can cross-check the typed Results against the
// original site in one click.
//
// It is a navigation convenience, deliberately not folded into the zone score.
// Spatial-ish — it needs the listing's coordinates, INSEE, or an address.
package links

// Categories grouping the links, returned in Link.Category.
const (
	CategoryMap      = "map"
	CategoryPrices   = "prices"
	CategoryRisks    = "risks"
	CategoryUrbanism = "urbanism"
	CategoryContext  = "context"
)

// Link is a single deep link to an external tool or dataset.
type Link struct {
	// Key is a stable identifier, e.g. "pappersimmo" or "georisques".
	Key string `json:"key"`

	// Label is a human-readable name for the destination.
	Label string `json:"label"`

	// Category groups links: map | prices | risks | urbanism | context.
	Category string `json:"category"`

	// URL is the deep link.
	URL string `json:"url"`
}

// Result is the typed payload returned by Source.Query: the set of deep links
// built for the listing, in a stable, concern-ordered sequence (map → prices →
// risks → urbanism → context).
type Result struct {
	// Links are the deep links.
	Links []Link `json:"links"`

	// Evidence captures which inputs the links were built from. Sidecar — not
	// wire data (json:"-").
	Evidence Evidence `json:"-"`
}

// Evidence records the inputs the links were derived from.
type Evidence struct {
	// Lat / Lon are the listing coordinates used for spatial links (0 when
	// none were available).
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`

	// INSEE is the commune code used for commune-level links.
	INSEE string `json:"insee,omitempty"`

	// Count is the number of links built.
	Count int `json:"count,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no link could be built
// (no usable inputs at all).
func (r *Result) IsEmpty() bool { return r == nil || len(r.Links) == 0 }

// Map returns the links as a key→URL map — the convenient shape for callers
// that just want to look one up by key (e.g. m["pappersimmo"]).
func (r *Result) Map() map[string]string {
	if r == nil {
		return nil
	}
	m := make(map[string]string, len(r.Links))
	for _, l := range r.Links {
		m[l.Key] = l.URL
	}
	return m
}

package dvf

import (
	"context"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/geopoly"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

func TestSectionDiscoverer_PrimeAndRead(t *testing.T) {
	d := NewSectionDiscoverer(memcache.New(), nil)

	if err := d.PrimeFromList(context.Background(), "12345", []string{"000AA", "000AB"}); err != nil {
		t.Fatalf("Prime: %v", err)
	}
	got, err := d.SectionsForCommune(context.Background(), "12345")
	if err != nil {
		t.Fatalf("SectionsForCommune: %v", err)
	}
	if len(got) != 2 || got[0] != "000AA" {
		t.Errorf("got %v", got)
	}
}

// SectionsForCommune returns nil (not an error) on a cache miss, so
// the caller (Source.resolveSections) can fall through to the cadastre
// primer.
func TestSectionDiscoverer_CacheMissReturnsNil(t *testing.T) {
	d := NewSectionDiscoverer(memcache.New(), nil)
	got, err := d.SectionsForCommune(context.Background(), "00000")
	if err != nil {
		t.Fatalf("expected nil error on cache miss, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice on cache miss, got %v", got)
	}
}

// TestSectionDiscoverer_GeoPrimeAndRead pins the reduced-geometry cache
// round-trip, including the empty (inverted-infinity ±Inf) box, which
// is not representable in plain JSON and must survive via the Empty
// wire flag — restored as an emptyBBox() so the "unknown extent ⇒ keep
// the section" prefilter semantics are preserved.
func TestSectionDiscoverer_GeoPrimeAndRead(t *testing.T) {
	d := NewSectionDiscoverer(memcache.New(), nil)
	ctx := context.Background()

	in := []SectionGeo{
		{Code: "000AA", Box: geopoly.BBox{MinLon: 2.0, MinLat: 48.0, MaxLon: 2.1, MaxLat: 48.1}},
		{Code: "0000B", Box: emptyBBox()},
	}
	if err := d.PrimeGeos(ctx, "12345", in); err != nil {
		t.Fatalf("PrimeGeos: %v", err)
	}
	got, err := d.GeosForCommune(ctx, "12345")
	if err != nil {
		t.Fatalf("GeosForCommune: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d geos, want 2: %+v", len(got), got)
	}
	if got[0] != in[0] {
		t.Errorf("geo[0] = %+v, want %+v", got[0], in[0])
	}
	if got[1].Code != "0000B" || !bboxEmpty(got[1].Box) {
		t.Errorf("geo[1] = %+v, want code 0000B with an empty (unknown-extent) box", got[1])
	}
}

// GeosForCommune mirrors SectionsForCommune's contract: (nil, nil) on a
// cache miss and on an expired row.
func TestSectionDiscoverer_GeoMissAndExpiry(t *testing.T) {
	d := NewSectionDiscoverer(memcache.New(), nil)
	ctx := context.Background()

	got, err := d.GeosForCommune(ctx, "00000")
	if err != nil || got != nil {
		t.Errorf("cache miss = (%v, %v), want (nil, nil)", got, err)
	}

	if err := d.PrimeGeos(ctx, "12345", []SectionGeo{{Code: "000AA"}}); err != nil {
		t.Fatalf("PrimeGeos: %v", err)
	}
	// Jump past the TTL: the row must be treated as a miss.
	d.now = func() time.Time { return time.Now().UTC().Add(SectionTTL + time.Hour) }
	got, err = d.GeosForCommune(ctx, "12345")
	if err != nil || got != nil {
		t.Errorf("expired row = (%v, %v), want (nil, nil)", got, err)
	}
}

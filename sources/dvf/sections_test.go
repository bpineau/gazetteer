package dvf

import (
	"context"
	"testing"

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

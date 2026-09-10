package cadastre

import (
	"fmt"
	"sync"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

func TestDefaultBatiCache_GetPutHappy(t *testing.T) {
	t.Parallel()

	c := &DefaultBatiCache{}
	if _, ok := c.Get("75104"); ok {
		t.Fatal("Get on empty cache returned ok=true")
	}
	want := []BatiPolygon{
		{
			Geometry: geopoly.MultiPolygon{{{{Lon: 0, Lat: 0}}}},
			Centroid: geopoly.Point{Lon: 0, Lat: 0},
			AreaM2:   42,
		},
	}
	c.Put("75104", want)
	got, ok := c.Get("75104")
	if !ok || len(got) != 1 || got[0].AreaM2 != 42 {
		t.Errorf("Get after Put = (%+v, %v), want length 1 / AreaM2=42", got, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Error("Get on unset key returned ok=true")
	}
}

// TestDefaultBatiCache_RaceFree ensures concurrent Get/Put under -race
// don't trip the detector: the map, the recency list and the lazily
// resolved ceiling all live behind one mutex, and Get reorders that list,
// so even the read path takes it.
func TestDefaultBatiCache_RaceFree(t *testing.T) {
	t.Parallel()

	c := &DefaultBatiCache{}
	var wg sync.WaitGroup
	const writers = 8
	const readers = 16
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			insee := "750" + string(rune('0'+i%10)) + "0"
			c.Put(insee, []BatiPolygon{{AreaM2: float64(i)}})
		}(i)
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Get("75100")
			_, _ = c.Get("75101")
		}()
	}
	wg.Wait()
}

// TestDefaultBatiCache_CapEvictsLeastRecentlyUsed: the values here are WHOLE
// commune dumps, so the cache is bounded. Past the ceiling, a new commune
// costs the least-recently-used one and nothing else. Before the cap, the
// sync.Map behind it grew with every commune the process ever saw.
func TestDefaultBatiCache_CapEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	c := &DefaultBatiCache{MaxCommunes: 2}
	c.Put("75101", []BatiPolygon{{AreaM2: 1}})
	c.Put("75102", []BatiPolygon{{AreaM2: 2}})

	// Touch 75101 so 75102 becomes the least-recently-used commune.
	if _, ok := c.Get("75101"); !ok {
		t.Fatal("Get(75101) missed right after Put")
	}
	c.Put("75103", []BatiPolygon{{AreaM2: 3}})

	if _, ok := c.Get("75102"); ok {
		t.Error("Get(75102) hit, want the least-recently-used commune evicted")
	}
	for _, insee := range []string{"75101", "75103"} {
		if _, ok := c.Get(insee); !ok {
			t.Errorf("Get(%s) missed, want the commune kept", insee)
		}
	}
}

// TestDefaultBatiCache_ZeroValueBounded pins the zero value's contract: ready
// to use (NewSource allocates one that way) and bounded to
// DefaultBatiCacheMaxCommunes, evicting rather than growing.
func TestDefaultBatiCache_ZeroValueBounded(t *testing.T) {
	t.Parallel()

	c := &DefaultBatiCache{}
	inseeAt := func(i int) string { return fmt.Sprintf("%05d", 10000+i) }
	for i := range DefaultBatiCacheMaxCommunes + 3 {
		c.Put(inseeAt(i), []BatiPolygon{{AreaM2: float64(i)}})
	}
	var kept int
	for i := range DefaultBatiCacheMaxCommunes + 3 {
		if _, ok := c.Get(inseeAt(i)); ok {
			kept++
		}
	}
	if kept != DefaultBatiCacheMaxCommunes {
		t.Errorf("kept %d communes, want the ceiling of %d", kept, DefaultBatiCacheMaxCommunes)
	}
	// The three oldest went; the newest is still there.
	if _, ok := c.Get(inseeAt(0)); ok {
		t.Error("the first commune survived a full cache, want it evicted")
	}
	if _, ok := c.Get(inseeAt(DefaultBatiCacheMaxCommunes + 2)); !ok {
		t.Error("the last commune inserted is missing")
	}
}

// TestDefaultBatiCache_NegativeMaxIsUnlimited: a short-lived process can trade
// memory for zero refetches.
func TestDefaultBatiCache_NegativeMaxIsUnlimited(t *testing.T) {
	t.Parallel()

	c := &DefaultBatiCache{MaxCommunes: -1}
	const n = DefaultBatiCacheMaxCommunes * 4
	for i := range n {
		c.Put(fmt.Sprintf("%05d", 10000+i), []BatiPolygon{{AreaM2: float64(i)}})
	}
	for i := range n {
		if _, ok := c.Get(fmt.Sprintf("%05d", 10000+i)); !ok {
			t.Fatalf("Get(%05d) missed, want every commune kept when unlimited", 10000+i)
		}
	}
}

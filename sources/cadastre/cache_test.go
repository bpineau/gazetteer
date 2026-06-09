package cadastre

import (
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
// don't trip the detector. sync.Map's own internals are race-free by
// design — the test exercises the wrapper layer (type assertion +
// nil-safe Load).
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

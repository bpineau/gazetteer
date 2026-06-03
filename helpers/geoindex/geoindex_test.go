package geoindex

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// square returns a single-polygon, single-ring Compact unit square with the
// given lower-left corner and side length.
func square(minLon, minLat, side float64) Compact {
	return Compact{{{
		{minLon, minLat},
		{minLon + side, minLat},
		{minLon + side, minLat + side},
		{minLon, minLat + side},
	}}}
}

func TestCompactMultiPolygonRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Compact
	}{
		{"nil", nil},
		{"empty", Compact{}},
		{"single square", square(2.0, 48.0, 0.01)},
		{
			"multipolygon with hole",
			Compact{
				{ // polygon 0: outer + hole
					{{0, 0}, {10, 0}, {10, 10}, {0, 10}},
					{{2, 2}, {4, 2}, {4, 4}, {2, 4}},
				},
				{ // polygon 1: a detached square
					{{20, 20}, {21, 20}, {21, 21}, {20, 21}},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mp := tc.in.MultiPolygon()
			got := FromMultiPolygon(mp)
			// Round-trip must preserve the exact vertex structure.
			if !compactEqual(got, tc.in) {
				t.Fatalf("round-trip mismatch:\n in = %v\nout = %v", tc.in, got)
			}
			// And the wire JSON shape must be the bare nested array, so committed
			// artifacts decode.
			b, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back Compact
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !compactEqual(back, tc.in) {
				t.Fatalf("json round-trip mismatch:\n in = %v\nout = %v", tc.in, back)
			}
		})
	}
}

func TestMultiPolygonStructure(t *testing.T) {
	t.Parallel()
	c := Compact{
		{{{1, 2}, {3, 4}, {5, 6}}},
	}
	mp := c.MultiPolygon()
	if len(mp) != 1 || len(mp[0]) != 1 || len(mp[0][0]) != 3 {
		t.Fatalf("unexpected shape: %#v", mp)
	}
	// [lon, lat] ordering must map to Point{Lon, Lat}.
	if mp[0][0][0] != (geopoly.Point{Lon: 1, Lat: 2}) {
		t.Fatalf("vertex 0 = %#v, want {Lon:1,Lat:2}", mp[0][0][0])
	}
}

func TestRoundCompact(t *testing.T) {
	t.Parallel()
	in := Compact{{{
		{2.123456789, 48.987654321},
		{2.1, 48.9},
	}}}
	got := RoundCompact(in, 4)
	want := Compact{{{
		{2.1235, 48.9877},
		{2.1, 48.9},
	}}}
	if !compactEqual(got, want) {
		t.Fatalf("RoundCompact(4):\n got = %v\nwant = %v", got, want)
	}
}

func TestDecodeGeoJSONGeometry(t *testing.T) {
	t.Parallel()
	t.Run("polygon", func(t *testing.T) {
		t.Parallel()
		coords := json.RawMessage(`[[[2.0,48.0],[2.1,48.0],[2.1,48.1]]]`)
		got, err := DecodeGeoJSONGeometry("Polygon", coords, 5)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got) != 1 || len(got[0]) != 1 || len(got[0][0]) != 3 {
			t.Fatalf("unexpected shape: %v", got)
		}
	})
	t.Run("multipolygon", func(t *testing.T) {
		t.Parallel()
		coords := json.RawMessage(`[[[[2.0,48.0],[2.1,48.0],[2.1,48.1]]],[[[3.0,49.0],[3.1,49.0],[3.1,49.1]]]]`)
		got, err := DecodeGeoJSONGeometry("MultiPolygon", coords, 5)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 polygons, got %d", len(got))
		}
	})
	t.Run("third ordinate dropped", func(t *testing.T) {
		t.Parallel()
		coords := json.RawMessage(`[[[2.0,48.0,100.0],[2.1,48.0,101.0],[2.1,48.1,102.0]]]`)
		got, err := DecodeGeoJSONGeometry("Polygon", coords, 5)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got[0][0][0] != [2]float64{2.0, 48.0} {
			t.Fatalf("altitude not dropped: %v", got[0][0][0])
		}
	})
	t.Run("malformed vertex dropped", func(t *testing.T) {
		t.Parallel()
		coords := json.RawMessage(`[[[2.0,48.0],[2.0],[2.1,48.1]]]`)
		got, err := DecodeGeoJSONGeometry("Polygon", coords, 5)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got[0][0]) != 2 {
			t.Fatalf("malformed vertex not dropped: %v", got[0][0])
		}
	})
	t.Run("rounding applied", func(t *testing.T) {
		t.Parallel()
		coords := json.RawMessage(`[[[2.123456,48.987654],[2.1,48.0],[2.2,48.0]]]`)
		got, err := DecodeGeoJSONGeometry("Polygon", coords, 4)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got[0][0][0] != [2]float64{2.1235, 48.9877} {
			t.Fatalf("rounding not applied: %v", got[0][0][0])
		}
	})
	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()
		if _, err := DecodeGeoJSONGeometry("Point", json.RawMessage(`[2,48]`), 5); err == nil {
			t.Fatalf("expected error for unsupported type")
		}
	})
}

// payload is a tiny test payload type for the generic Index.
type payload struct {
	id string
}

func TestIndexResolve(t *testing.T) {
	t.Parallel()
	idx := New([]Feature[payload]{
		NewFeature(payload{"A"}, square(0, 0, 10).MultiPolygon()),
		NewFeature(payload{"B"}, square(5, 5, 10).MultiPolygon()), // overlaps A in [5,10]x[5,10]
		NewFeature(payload{"C"}, square(100, 100, 1).MultiPolygon()),
	})

	t.Run("inside first", func(t *testing.T) {
		t.Parallel()
		got, ok := idx.Resolve(2, 2) // lat=2, lon=2
		if !ok || got.id != "A" {
			t.Fatalf("Resolve(2,2) = %v,%v want A", got, ok)
		}
	})
	t.Run("inside second only", func(t *testing.T) {
		t.Parallel()
		got, ok := idx.Resolve(12, 12)
		if !ok || got.id != "B" {
			t.Fatalf("Resolve(12,12) = %v,%v want B", got, ok)
		}
	})
	t.Run("overlap first-cover wins deterministically", func(t *testing.T) {
		t.Parallel()
		// (7,7) is inside both A and B; A comes first in feature order.
		got, ok := idx.Resolve(7, 7)
		if !ok || got.id != "A" {
			t.Fatalf("Resolve(7,7) = %v,%v want A (first-cover)", got, ok)
		}
	})
	t.Run("outside all", func(t *testing.T) {
		t.Parallel()
		_, ok := idx.Resolve(-50, -50)
		if ok {
			t.Fatalf("Resolve outside should be false")
		}
	})
	t.Run("nil index", func(t *testing.T) {
		t.Parallel()
		var nilIdx *Index[payload]
		if _, ok := nilIdx.Resolve(1, 1); ok {
			t.Fatalf("nil index Resolve should be false")
		}
		if nilIdx.Len() != 0 {
			t.Fatalf("nil index Len should be 0")
		}
	})
}

func TestIndexResolveWhere(t *testing.T) {
	t.Parallel()
	idx := New([]Feature[payload]{
		NewFeature(payload{"A"}, square(0, 0, 10).MultiPolygon()),
		NewFeature(payload{"B"}, square(0, 0, 10).MultiPolygon()), // identical geometry
	})
	// Without a predicate, A wins; with a predicate selecting B, B wins.
	got, ok := idx.ResolveWhere(2, 2, func(p payload) bool { return p.id == "B" })
	if !ok || got.id != "B" {
		t.Fatalf("ResolveWhere = %v,%v want B", got, ok)
	}
	_, ok = idx.ResolveWhere(2, 2, func(p payload) bool { return p.id == "Z" })
	if ok {
		t.Fatalf("ResolveWhere with no match should be false")
	}
}

func TestIndexNearest(t *testing.T) {
	t.Parallel()
	idx := New([]Feature[payload]{
		NewFeature(payload{"A"}, square(0, 0, 0.001).MultiPolygon()),
		NewFeature(payload{"B"}, square(1, 1, 0.001).MultiPolygon()),
	})
	// A point just outside A's corner; A is far nearer than B.
	got, dist, ok := idx.Nearest(0, 0, 100000)
	if !ok || got.id != "A" {
		t.Fatalf("Nearest = %v,%v,%v want A", got, dist, ok)
	}
	if dist < 0 {
		t.Fatalf("distance should be non-negative, got %v", dist)
	}
	// A tight cap excludes everything.
	if _, _, ok := idx.Nearest(50, 50, 1); ok {
		t.Fatalf("Nearest beyond cap should be false")
	}
}

// helpers ------------------------------------------------------------------

func compactEqual(a, b Compact) bool {
	// Treat nil and empty as equal at the top level for round-trip checks.
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if len(a[i][j]) != len(b[i][j]) {
				return false
			}
			for k := range a[i][j] {
				if math.Abs(a[i][j][k][0]-b[i][j][k][0]) > 1e-12 ||
					math.Abs(a[i][j][k][1]-b[i][j][k][1]) > 1e-12 {
					return false
				}
			}
		}
	}
	return true
}

package geopoly

import "testing"

func TestRepresentativePoint(t *testing.T) {
	t.Parallel()
	// An L-shape: its shoelace centroid falls in the notch, outside the ring.
	l := Ring{
		{Lon: 2.3500, Lat: 48.8500}, {Lon: 2.3510, Lat: 48.8500},
		{Lon: 2.3510, Lat: 48.8503}, {Lon: 2.3503, Lat: 48.8503},
		{Lon: 2.3503, Lat: 48.8510}, {Lon: 2.3500, Lat: 48.8510},
	}
	poly := Polygon{l}
	if poly.Covers(poly.Centroid()) {
		t.Fatalf("fixture is not concave enough: centroid %v is inside", poly.Centroid())
	}
	p, ok := poly.RepresentativePoint()
	if !ok {
		t.Fatalf("RepresentativePoint: ok=false on a non-empty L")
	}
	if !poly.Covers(p) {
		t.Errorf("RepresentativePoint %v is NOT inside the polygon", p)
	}
	// A polygon with a hole: the point must not land in the courtyard.
	outer := Ring{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}, {Lon: 0, Lat: 1}}
	hole := Ring{{Lon: 0.2, Lat: 0.2}, {Lon: 0.8, Lat: 0.2}, {Lon: 0.8, Lat: 0.8}, {Lon: 0.2, Lat: 0.8}}
	ring := Polygon{outer, hole}
	p, ok = ring.RepresentativePoint()
	if !ok || !ring.Covers(p) {
		t.Errorf("ring-shaped polygon: point %v ok=%v, Covers=%v", p, ok, ring.Covers(p))
	}
	// Degenerate shapes report failure rather than a bogus point.
	if _, ok := (Polygon{}).RepresentativePoint(); ok {
		t.Error("empty polygon: ok=true")
	}
	if _, ok := (Polygon{Ring{{Lon: 1, Lat: 1}}}).RepresentativePoint(); ok {
		t.Error("degenerate ring: ok=true")
	}
	// A plain square still returns its centroid.
	sq := Polygon{Ring{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}, {Lon: 0, Lat: 1}}}
	if p, ok := sq.RepresentativePoint(); !ok || p != sq.Centroid() {
		t.Errorf("square: got %v ok=%v, want the centroid %v", p, ok, sq.Centroid())
	}
}

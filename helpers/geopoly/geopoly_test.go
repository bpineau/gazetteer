package geopoly

import "testing"

// unitSquare is the axis-aligned [0,1]×[0,1] square (counter-clockwise),
// left implicitly closed (first point not repeated) to exercise the
// implicit-closing path.
var unitSquare = Polygon{{{0, 0}, {1, 0}, {1, 1}, {0, 1}}}

func TestPolygonCoversSquare(t *testing.T) {
	cases := []struct {
		name string
		p    Point
		want bool
	}{
		{"center", Point{0.5, 0.5}, true},
		{"near left edge inside", Point{0.01, 0.5}, true},
		{"left of square", Point{-0.5, 0.5}, false},
		{"right of square", Point{1.5, 0.5}, false},
		{"above square", Point{0.5, 1.5}, false},
		{"below square", Point{0.5, -0.5}, false},
		{"far away", Point{10, 10}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unitSquare.Covers(c.p); got != c.want {
				t.Errorf("Covers(%v) = %v, want %v", c.p, got, c.want)
			}
		})
	}
}

// TestPolygonCoversConcave checks ray casting on a non-convex (L-shaped)
// polygon, where a naive convex test would wrongly include the notch.
func TestPolygonCoversConcave(t *testing.T) {
	// L-shape: full bottom (0..2 × 0..1) plus left column (0..1 × 1..2).
	// The notch (1..2 × 1..2) is OUTSIDE.
	l := Polygon{{{0, 0}, {2, 0}, {2, 1}, {1, 1}, {1, 2}, {0, 2}}}
	cases := []struct {
		name string
		p    Point
		want bool
	}{
		{"bottom bar", Point{1.5, 0.5}, true},
		{"left column", Point{0.5, 1.5}, true},
		{"notch is outside", Point{1.5, 1.5}, false},
		{"corner of notch outside", Point{1.9, 1.9}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := l.Covers(c.p); got != c.want {
				t.Errorf("Covers(%v) = %v, want %v", c.p, got, c.want)
			}
		})
	}
}

// TestPolygonCoversHole checks that a point inside a hole is reported as not
// covered, while a point in the surrounding band is covered.
func TestPolygonCoversHole(t *testing.T) {
	// Outer 0..3 square with an inner 1..2 hole.
	poly := Polygon{
		{{0, 0}, {3, 0}, {3, 3}, {0, 3}}, // outer
		{{1, 1}, {2, 1}, {2, 2}, {1, 2}}, // hole
	}
	cases := []struct {
		name string
		p    Point
		want bool
	}{
		{"in band", Point{0.5, 0.5}, true},
		{"in band right", Point{2.5, 1.5}, true},
		{"in hole", Point{1.5, 1.5}, false},
		{"outside", Point{5, 5}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := poly.Covers(c.p); got != c.want {
				t.Errorf("Covers(%v) = %v, want %v", c.p, got, c.want)
			}
		})
	}
}

func TestMultiPolygonCovers(t *testing.T) {
	// Two disjoint unit squares: one at origin, one shifted to x∈[5,6].
	mp := MultiPolygon{
		{{{0, 0}, {1, 0}, {1, 1}, {0, 1}}},
		{{{5, 0}, {6, 0}, {6, 1}, {5, 1}}},
	}
	cases := []struct {
		name string
		p    Point
		want bool
	}{
		{"in first", Point{0.5, 0.5}, true},
		{"in second", Point{5.5, 0.5}, true},
		{"in the gap", Point{3, 0.5}, false},
		{"nil multipolygon", Point{0.5, 0.5}, true}, // overwritten below
	}
	for _, c := range cases[:3] {
		t.Run(c.name, func(t *testing.T) {
			if got := mp.Covers(c.p); got != c.want {
				t.Errorf("Covers(%v) = %v, want %v", c.p, got, c.want)
			}
		})
	}
	t.Run("nil multipolygon covers nothing", func(t *testing.T) {
		var empty MultiPolygon
		if empty.Covers(Point{0.5, 0.5}) {
			t.Error("nil MultiPolygon.Covers = true, want false")
		}
	})
}

// TestDegenerateRingsIgnored ensures rings with fewer than 3 points do not
// panic and contribute no crossings.
func TestDegenerateRingsIgnored(t *testing.T) {
	poly := Polygon{
		{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, // valid outer
		{{0.5, 0.5}, {0.6, 0.5}},         // degenerate (2 pts) — ignored
		{},                               // empty — ignored
	}
	if !poly.Covers(Point{0.5, 0.5}) {
		t.Error("degenerate inner rings should not affect coverage")
	}
}

func TestBound(t *testing.T) {
	mp := MultiPolygon{
		{{{0, 0}, {1, 0}, {1, 1}, {0, 1}}},
		{{{5, -2}, {6, -2}, {6, 3}, {5, 3}}},
	}
	got := mp.Bound()
	want := BBox{MinLon: 0, MinLat: -2, MaxLon: 6, MaxLat: 3}
	if got != want {
		t.Errorf("Bound() = %+v, want %+v", got, want)
	}
	if !got.Contains(Point{3, 0}) {
		t.Error("BBox.Contains(inside) = false")
	}
	if got.Contains(Point{7, 0}) {
		t.Error("BBox.Contains(outside) = true")
	}
}

// TestBoundSpansAllRings guards against a bbox that only spans ring 0: real
// data sometimes packs disjoint regions into one Polygon's rings, and a
// non-zero ring can extend past ring 0 (e.g. Montreuil rent-control zone 308).
// A box missing those rings would make a Covers-prefilter reject covered points.
func TestBoundSpansAllRings(t *testing.T) {
	mp := MultiPolygon{{
		{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, // ring 0
		{{5, 5}, {6, 5}, {6, 6}, {5, 6}}, // ring 1 — disjoint, far past ring 0
	}}
	got := mp.Bound()
	want := BBox{MinLon: 0, MinLat: 0, MaxLon: 6, MaxLat: 6}
	if got != want {
		t.Fatalf("Bound() = %+v, want %+v (must span ring 1)", got, want)
	}
	p := Point{5.5, 5.5}
	if !got.Contains(p) {
		t.Error("Bound() excludes ring-1 point — a bbox prefilter would wrongly reject it")
	}
	if !mp.Covers(p) {
		t.Error("Covers(ring-1 point) = false")
	}
}

// TestEmptyBound returns the zero BBox for an empty MultiPolygon and reports
// Contains == false for any point (so callers can use it as a cheap reject).
func TestEmptyBound(t *testing.T) {
	var mp MultiPolygon
	b := mp.Bound()
	if b.Contains(Point{0, 0}) {
		t.Error("empty Bound().Contains = true, want false")
	}
}

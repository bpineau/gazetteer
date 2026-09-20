package communes

import (
	"slices"
	"sort"
	"testing"
)

// TestNeighbors_CrossesDepartmentBorders is the regression for the
// prefilter that used to decide which communes existed.
//
// Neighbors scanned the querying commune's OWN département and widened
// to the rest of France only above a 10 km radius. A commune on a
// département boundary has half its neighbours on the other side, so
// the answer was short by exactly the communes a border address cares
// about — and the DVF "neighborhood" tier runs at 5 km, documented as
// "communes within 5 km", so it priced a market half the size.
//
// The reference is a brute-force haversine over the whole table: the
// answer Neighbors promises, spelled out.
func TestNeighbors_CrossesDepartmentBorders(t *testing.T) {
	t.Parallel()
	tbl := MustDefault()

	// Bezons (95063) sits on the Seine facing the Hauts-de-Seine and the
	// Yvelines; before the fix it saw 2 of its 10 neighbours.
	// Paray-Vieille-Poste (91479) is wedged against the Val-de-Marne: 7
	// of 14.
	for _, insee := range []string{"95063", "91479", "78646", "77288", "59350", "75107"} {
		got := tbl.Neighbors(insee, 5.0)
		want := bruteForceNeighbors(tbl, insee, 5.0)
		if len(got) != len(want) {
			t.Errorf("Neighbors(%s, 5) returned %d communes, want %d (the ones actually within 5 km)", insee, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("Neighbors(%s, 5)[%d] = %s, want %s", insee, i, got[i], want[i])
				break
			}
		}
	}
}

// TestNeighbors_SelfAndUnknown keeps the contract's edges: the querying
// commune is always in the list, an unknown code answers nil.
func TestNeighbors_SelfAndUnknown(t *testing.T) {
	t.Parallel()
	tbl := MustDefault()
	got := tbl.Neighbors("95063", 5.0)
	if !slices.Contains(got, "95063") {
		t.Errorf("Neighbors(95063, 5) = %v, want it to contain itself", got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Neighbors(95063, 5) = %v, want it sorted", got)
	}
	if got := tbl.Neighbors("00000", 5.0); got != nil {
		t.Errorf("Neighbors(unknown) = %v, want nil", got)
	}
}

// bruteForceNeighbors is Neighbors' own contract, written the slow and
// obvious way: every commune within the radius, sorted.
func bruteForceNeighbors(t *Table, insee string, radiusKm float64) []string {
	c, ok := t.Lookup(insee)
	if !ok {
		return nil
	}
	var out []string
	for _, o := range t.All() {
		if HaversineKm(c.Lat, c.Lon, o.Lat, o.Lon) <= radiusKm {
			out = append(out, o.INSEE)
		}
	}
	sort.Strings(out)
	return out
}

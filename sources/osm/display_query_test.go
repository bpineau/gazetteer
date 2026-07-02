package osm

import (
	"context"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestStationDisplay(t *testing.T) {
	cases := []struct {
		st   Station
		want string
	}{
		{Station{Name: "Lourmel"}, "Lourmel"},
		{Station{Name: "Lourmel", Type: TransitTypeMetro, Lines: []string{"8"}}, "Lourmel (M8)"},
		{Station{Name: "Châtelet", Type: TransitTypeMetro, Lines: []string{"1", "4"}}, "Châtelet (M1/4)"},
		{Station{Name: "Vincennes", Type: TransitTypeRER, Lines: []string{"A"}}, "Vincennes (RER A)"},
		{Station{Name: "Porte de Choisy", Type: TransitTypeTram, Lines: []string{"3a"}}, "Porte de Choisy (T3a)"},
		{Station{Name: "Meaux", Type: TransitTypeTransilien, Lines: []string{"P"}}, "Meaux (TP)"},
	}
	for _, c := range cases {
		if got := c.st.Display(); got != c.want {
			t.Errorf("Display(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}

func TestOverpassQLBuilders(t *testing.T) {
	ql := FranceTransitOverpassQL("")
	for _, want := range []string{
		"[out:json]", "[bbox:" + FranceMetropolitanBBox + "]",
		`node["railway"="station"]`, "out center tags;",
	} {
		if !strings.Contains(ql, want) {
			t.Errorf("stations QL missing %q", want)
		}
	}

	routes := FranceTransitRoutesOverpassQL("41.0,-5.5,51.5,10.0")
	for _, want := range []string{
		"[bbox:41.0,-5.5,51.5,10.0]",
		`relation["type"="route"]["route"="subway"]`,
		`relation["public_transport"="stop_area"]`,
		"out body;",
	} {
		if !strings.Contains(routes, want) {
			t.Errorf("routes QL missing %q", want)
		}
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", 42: "42", 1048576: "1048576"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestQueryResult_EmbeddedCatalog runs the full typed query path against
// the embedded station catalog, offline (no Fetcher, so no live
// fallback): a point in central Paris must resolve to a nearby station.
func TestQueryResult_EmbeddedCatalog(t *testing.T) {
	s := NewSource(Options{})
	lat, lon := 48.8583, 2.3470 // Châtelet
	r, err := s.QueryResult(context.Background(), gazetteer.Listing{Lat: &lat, Lon: &lon})
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	if r.IsEmpty() {
		t.Fatal("central Paris must find a station in the embedded catalog")
	}

	// Missing coordinates: the source needs lat/lon.
	if _, err := s.QueryResult(context.Background(), gazetteer.Listing{}); err == nil {
		t.Error("no coordinates should be an insufficient-inputs error")
	}
}

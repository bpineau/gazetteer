package links_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/links"
)

func TestQueryFromCoords(t *testing.T) {
	res, err := links.Query(context.Background(), links.Options{}, gazetteer.Listing{
		Lat: new(48.873128), Lon: new(2.352599), INSEE: "75102", City: "Paris",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatal("expected links")
	}

	m := res.Map()
	// A coordinate-based link the user explicitly asked for.
	if got := m["pappersimmo"]; !strings.Contains(got, "lat=48.873128") || !strings.Contains(got, "lon=2.352599") {
		t.Errorf("pappersimmo url = %q, want lat/lon embedded", got)
	}
	// INSEE-based context link.
	if got := m["insee_commune"]; !strings.HasSuffix(got, "COM75102") {
		t.Errorf("insee_commune url = %q, want COM75102 suffix", got)
	}
	// Géorisques is enriched with INSEE + city when present.
	if got := m["georisques"]; !strings.Contains(got, "codeInsee=75102") || !strings.Contains(got, "city=Paris") {
		t.Errorf("georisques url = %q, want codeInsee + city", got)
	}
	// Every link must carry the four fields.
	for _, l := range res.Links {
		if l.Key == "" || l.Label == "" || l.Category == "" || l.URL == "" {
			t.Errorf("incomplete link: %+v", l)
		}
	}
}

// TestGeoportailLinksHonorCenter guards the Géoportail deep links: the SPA only
// restores the center/zoom when permalink=yes is present — without it both the
// ortho and cadastre links open on the whole-France view (reported bug). The
// ortho link must also actually select the orthophoto layer.
func TestGeoportailLinksHonorCenter(t *testing.T) {
	res, err := links.Query(context.Background(), links.Options{}, gazetteer.Listing{
		Lat: new(48.861396), Lon: new(2.474050),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	m := res.Map()
	for _, key := range []string{"geoportail", "cadastre"} {
		u := m[key]
		if !strings.Contains(u, "c=2.474050,48.861396") {
			t.Errorf("%s url = %q, want center c=lon,lat", key, u)
		}
		if !strings.Contains(u, "permalink=yes") {
			t.Errorf("%s url = %q, missing permalink=yes (center is ignored without it)", key, u)
		}
	}
	if u := m["geoportail"]; !strings.Contains(u, "ORTHOIMAGERY.ORTHOPHOTOS") {
		t.Errorf("geoportail (ortho) url = %q, want the orthophoto layer", u)
	}
	if u := m["cadastre"]; !strings.Contains(u, "CADASTRALPARCELS.PARCELLAIRE_EXPRESS") {
		t.Errorf("cadastre url = %q, want the cadastral-parcels layer", u)
	}
}

func TestQueryInseeOnly(t *testing.T) {
	res, err := links.Query(context.Background(), links.Options{}, gazetteer.Listing{INSEE: "93048"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	m := res.Map()
	if _, ok := m["insee_commune"]; !ok {
		t.Error("expected insee_commune link from INSEE alone")
	}
	if _, ok := m["pappersimmo"]; ok {
		t.Error("did not expect coordinate links without Lat/Lon")
	}
}

func TestQueryInsufficientInputs(t *testing.T) {
	_, err := links.Query(context.Background(), links.Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

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

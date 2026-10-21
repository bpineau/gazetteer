package bdnb

import (
	"context"
	"net/http"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// The ilike query returns every building on the street. When the listing's
// house number is not among them, the most complete row used to be picked:
// another building's construction year, dwelling count and DPE, returned as
// the listing's at medium confidence. The number names the building; without
// it on any row there is no match to report.
func TestSource_NumberNotOnTheStreetIsNoMatch(t *testing.T) {
	t.Parallel()
	body := []byte(`[
        {"batiment_groupe_id":"a","cle_interop_adr_principale_ban":"X_8","libelle_adr_principale_ban":"8 Rue Aubert 93200 Saint-Denis","code_commune_insee":"93066","annee_construction":1910,"nb_log":12},
        {"batiment_groupe_id":"c","cle_interop_adr_principale_ban":"X_10","libelle_adr_principale_ban":"10 Rue Aubert 93200 Saint-Denis","code_commune_insee":"93066","annee_construction":1975}
    ]`)
	srv := newStubServer(t, http.StatusOK, body)
	s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{cityCode: "93066"}, HTTPClient: srv.Client()})
	data, err := s.Query(context.Background(), gazetteer.Listing{Address: "9, rue Aubert 93200 Saint-Denis", Zip: "93200"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() || !res.Skipped || res.SkipReason != SkipReasonNumberNotFound {
		t.Fatalf("got %+v (identity %+v), want a skipped %q result", res, res.Identity, SkipReasonNumberNotFound)
	}

	// Without a house number the street-level pick stands (as before).
	data, err = s.Query(context.Background(), gazetteer.Listing{Address: "rue Aubert 93200 Saint-Denis", Zip: "93200"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res := data.(*Result); res.IsEmpty() {
		t.Error("no house number: IsEmpty(), want the street-level pick")
	}
}

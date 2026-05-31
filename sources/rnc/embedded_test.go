package rnc

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestEmbedded_KeyedByCOGNotPostal guards the most damaging RNC footgun: the
// upstream "code_officiel_commune" column actually holds the POSTAL code, while
// the true INSEE (COG) lives in "code_officiel_arrondissement_commune". The
// transform must key on the latter — otherwise ~62 % of copropriétés sit under
// a postal code no query ever supplies, and every commune whose code postal
// differs from its code INSEE (the vast majority, incl. all of Paris/Lyon/
// Marseille intra-muros) silently returns "no copropriété matched".
//
// We assert the embedded national artifact buckets a handful of well-known
// communes under their COG INSEE and NOT under their postal code.
func TestEmbedded_KeyedByCOGNotPostal(t *testing.T) {
	idx, err := Load("") // embedded artifact — same path the CLI binary takes
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx.Count() == 0 {
		t.Fatal("embedded RNC index is empty")
	}

	cases := []struct {
		name, cog, postal string
	}{
		{"Paris 10e", "75110", "75010"},
		{"Cannes", "06029", "06400"},
		{"Biarritz", "64122", "64200"},
		{"Boulogne-Billancourt", "92012", "92100"},
	}
	for _, c := range cases {
		if len(idx.ByInsee[c.cog]) == 0 {
			t.Errorf("%s: ByInsee[%q] (COG) is empty — transform is keying on the wrong column", c.name, c.cog)
		}
		if n := len(idx.ByInsee[c.postal]); n != 0 {
			t.Errorf("%s: ByInsee[%q] (postal) has %d rows — should be keyed by COG, not code postal", c.name, c.postal, n)
		}
	}
}

// TestEmbedded_Query_ParisAddress is the end-to-end regression for the reported
// bug: a real Paris address (whose normalizer-supplied INSEE is 75110) must
// resolve to its copropriété instead of coming back empty.
func TestEmbedded_Query_ParisAddress(t *testing.T) {
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{
		INSEE:   "75110",
		Lat:     f64(48.873128),
		Lon:     f64(2.353599),
		Address: "8 rue des petites ecuries 75010 paris",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatal("expected a copropriété match for a real Paris address, got empty")
	}
	t.Logf("matched imm=%s nom=%q conf=%s dist=%.0fm",
		res.Immatriculation, res.NomUsage, res.Confidence, res.Evidence.MatchDistance)
}

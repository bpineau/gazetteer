package encadrement

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// TestUncoveredTerritories_CodesNameTheDecreedCommunes guards the hand-kept
// INSEE list against a transcription slip: every code must name, in the
// communes table, the commune its decree names (the comment beside it), and
// no code may sit in two territories or in a shipped grille.
func TestUncoveredTerritories_CodesNameTheDecreedCommunes(t *testing.T) {
	t.Parallel()
	tbl, err := communes.Default()
	if err != nil {
		t.Fatalf("communes: %v", err)
	}
	want := map[string]string{
		"59350": "Lille", "34172": "Montpellier", "33063": "Bordeaux",
		"64009": "Ahetze", "64024": "Anglet", "64035": "Arbonne", "64038": "Arcangues",
		"64065": "Ascain", "64100": "Bassussarry", "64102": "Bayonne", "64122": "Biarritz",
		"64125": "Bidart", "64130": "Biriatou", "64140": "Boucau", "64189": "Ciboure",
		"64249": "Guéthary", "64260": "Hendaye", "64282": "Jatxou", "64304": "Lahonce",
		"64317": "Larressore", "64407": "Mouguerre", "64483": "Saint-Jean-de-Luz",
		"64496": "Saint-Pierre-d'Irube", "64540": "Urcuit", "64545": "Urrugne",
		"64547": "Ustaritz", "64558": "Villefranque",
		"38057": "Bresson", "38111": "Claix", "38150": "Domène", "38158": "Eybens",
		"38170": "Fontanil-Cornillon", "38179": "Gières", "38229": "Meylan",
		"38271": "Murianette", "38309": "Poisat", "38486": "Seyssins", "38516": "La Tronche",
		"38524": "Varces-Allières-et-Risset", "38533": "Venon",
		"38151": "Échirolles", "38169": "Fontaine", "38185": "Grenoble",
		"38317": "Le Pont-de-Claix", "38382": "Saint-Égrève", "38421": "Saint-Martin-d'Hères",
		"38474": "Sassenage", "38485": "Seyssinet-Pariset",
	}
	// Former codes of Lille's communes associées: absent from the current
	// table by construction.
	former := map[string]bool{"59298": true, "59355": true}

	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seen := map[string]string{}
	counts := map[string]int{}
	for _, terr := range uncoveredTerritories {
		for _, c := range append(append([]string{}, terr.whole...), terr.partial...) {
			counts[terr.name]++
			if other, dup := seen[c]; dup {
				t.Errorf("%s listed in both %s and %s", c, other, terr.name)
			}
			seen[c] = terr.name
			if _, _, inEPT := idx.ZonesForINSEE(c); inEPT || len(idx.LookupLyonInsee(c)) > 0 || parisArrondissementFromINSEE(c) != "" {
				t.Errorf("%s (%s) is also a shipped zone", c, terr.name)
			}
			if former[c] {
				continue
			}
			name, ok := want[c]
			if !ok {
				t.Errorf("%s (%s): no expected name, add it to this test from the decree", c, terr.name)
				continue
			}
			got, ok := tbl.Lookup(c)
			if !ok || got.Name != name {
				t.Errorf("%s: communes table names %q, the decree %q", c, got.Name, name)
			}
		}
	}
	// The decrees' own counts: Pays basque 24 communes (décret 2023-981),
	// Grenoble-Alpes Métropole 13 in full plus 8 in part (décret 2023-1046).
	for name, n := range map[string]int{"Pays basque": 24, "Grenoble-Alpes Métropole": 21, "Bordeaux": 1, "Montpellier": 1} {
		if counts[name] != n {
			t.Errorf("%s: %d communes, want %d", name, counts[name], n)
		}
	}
}

// Bordeaux, Montpellier, Lille, the Pays basque and Grenoble-Alpes Métropole
// cap rents under art. 140 of loi 2018-1021, but the Source ships no grille for
// them, and used to answer exactly as for an unregulated commune (IsEmpty,
// ConfidenceNone): a consumer read "no cap" where the law sets one. They now
// read as regulated with an unknown cap.
func TestQuery_DecreedTerritoryWithoutGrille(t *testing.T) {
	t.Parallel()
	cases := []struct {
		l         gazetteer.Listing
		reg, terr string
	}{
		{gazetteer.Listing{INSEE: "33063", Zip: "33000", Rooms: intp(2)}, RegulationNotCovered, "Bordeaux"},
		{gazetteer.Listing{INSEE: "34172", Rooms: intp(2)}, RegulationNotCovered, "Montpellier"},
		{gazetteer.Listing{INSEE: "59350", Rooms: intp(2)}, RegulationNotCovered, "Lille (Hellemmes, Lomme)"},
		{gazetteer.Listing{INSEE: "59355"}, RegulationNotCovered, "Lille (Hellemmes, Lomme)"},
		{gazetteer.Listing{INSEE: "64122", Rooms: intp(2)}, RegulationNotCovered, "Pays basque"},
		{gazetteer.Listing{INSEE: "38229"}, RegulationNotCovered, "Grenoble-Alpes Métropole"},
		{gazetteer.Listing{INSEE: "38185", IRIS: "381850101"}, RegulationPartialCommune, "Grenoble-Alpes Métropole"},
		// A set INSEE stays authoritative here too.
		{gazetteer.Listing{INSEE: "33063", Zip: "75001"}, RegulationNotCovered, "Bordeaux"},
	}
	for _, c := range cases {
		r := query(t, c.l)
		if r.IsEmpty() {
			t.Errorf("%s: IsEmpty, want a regulated reading", c.l.INSEE)
		}
		if r.Regulation != c.reg || r.Territory != c.terr {
			t.Errorf("%s: Regulation %q Territory %q, want %q %q", c.l.INSEE, r.Regulation, r.Territory, c.reg, c.terr)
		}
		if r.LoyerRefMajEURPerM2HC != 0 || r.LoyerRefMajMeubleEURPerM2HC != 0 || r.Confidence != ConfidenceNone {
			t.Errorf("%s: maj %.2f meublé %.2f conf %q, want no figure at ConfidenceNone", c.l.INSEE, r.LoyerRefMajEURPerM2HC, r.LoyerRefMajMeubleEURPerM2HC, r.Confidence)
		}
		if r.Evidence.Note == "" {
			t.Errorf("%s: no Evidence.Note", c.l.INSEE)
		}
		if _, ok := r.RentCap(); ok {
			t.Errorf("%s: RentCap ok, want no cap figure", c.l.INSEE)
		}
		if e := r.RentEstimate(); e.EurPerM2Cents != 0 {
			t.Errorf("%s: RentEstimate %d cents, want 0 (nothing to blend)", c.l.INSEE, e.EurPerM2Cents)
		}
	}
	if name, partial, ok := UncoveredTerritory("38185"); !ok || !partial || name != "Grenoble-Alpes Métropole" {
		t.Errorf("UncoveredTerritory(38185) = %q %v %v", name, partial, ok)
	}
	if _, _, ok := UncoveredTerritory("75101"); ok {
		t.Error("UncoveredTerritory(75101) ok: Paris ships a grille")
	}
}

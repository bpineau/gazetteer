package encadrement

import (
	"fmt"
	"math"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestGrilles_MeubleCellsPairOneToOne is the mechanical check over every
// meublé cell the Source ships: the furnished cap is now returned, so a
// transcription slip on one of those cells would be read as law.
//
// For every grille (Paris, Plaine Commune, Est Ensemble, Lyon / Villeurbanne):
//
//   - each meublé cell has exactly one unfurnished twin (same zone, pièces,
//     époque, maison flag) and each unfurnished cell exactly one meublé twin;
//   - on both sides, majoré = 1.2 x référence and minoré = 0.7 x référence,
//     to the published 0.1 rounding (the definition in art. 140 of loi
//     2018-1021: the majoré is the référence plus 20 %, the minoré the
//     référence less 30 %);
//   - the meublé référence is at least the unfurnished one (the arrêtés derive
//     it by adding a non-negative "majoration unitaire" per m²).
func TestGrilles_MeubleCellsPairOneToOne(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	grilles := map[string]map[string][]Entry{
		ZoneSourceParis:            idx.byArrondissement,
		ZoneSourcePlaineCommune:    idx.byPlaineCommuneZone,
		ZoneSourceEstEnsemble:      idx.byEstEnsembleZone,
		ZoneSourceLyonVilleurbanne: idx.byLyonIRIS,
	}
	type cellKey struct {
		zone      string
		piece     int
		openEnded bool
		epoque    string
		maison    bool
	}
	// withinRounding accepts a published figure that is f x ref rounded to
	// one decimal (half a step, plus float slack).
	withinRounding := func(got, ref, f float64) bool {
		return math.Abs(got-ref*f) <= 0.05+1e-9
	}
	for name, byZone := range grilles {
		cells := map[cellKey][2][]Entry{} // [0] unfurnished, [1] meublé
		n := 0
		for _, entries := range byZone {
			for _, e := range entries {
				k := cellKey{e.ZoneID, e.Piece, e.PieceOpenEnded, e.Epoque, e.Maison}
				pair := cells[k]
				side := 0
				if e.Meuble {
					side = 1
				}
				pair[side] = append(pair[side], e)
				cells[k] = pair
				n++
				label := fmt.Sprintf("%s %+v meublé=%v", name, k, e.Meuble)
				if !withinRounding(e.LoyerRefMaxEURPerM2HC, e.LoyerRefEURPerM2HC, 1.2) {
					t.Errorf("%s: majoré %.2f is not 1.2 x référence %.2f", label, e.LoyerRefMaxEURPerM2HC, e.LoyerRefEURPerM2HC)
				}
				if !withinRounding(e.LoyerRefMinEURPerM2HC, e.LoyerRefEURPerM2HC, 0.7) {
					t.Errorf("%s: minoré %.2f is not 0.7 x référence %.2f", label, e.LoyerRefMinEURPerM2HC, e.LoyerRefEURPerM2HC)
				}
			}
		}
		if n == 0 {
			t.Errorf("%s: no cell loaded", name)
			continue
		}
		meuble := 0
		for k, pair := range cells {
			if len(pair[0]) != 1 || len(pair[1]) != 1 {
				t.Errorf("%s %+v: %d unfurnished and %d meublé cells, want one of each", name, k, len(pair[0]), len(pair[1]))
				continue
			}
			meuble++
			if nu, mb := pair[0][0].LoyerRefEURPerM2HC, pair[1][0].LoyerRefEURPerM2HC; mb < nu {
				t.Errorf("%s %+v: meublé référence %.2f below the unfurnished %.2f", name, k, mb, nu)
			}
		}
		if 2*meuble != n {
			t.Errorf("%s: %d cells, %d complete pairs", name, n, meuble)
		}
	}
}

// TestBaremeVintage_SpotChecksMeuble pins meublé cells against the published
// arrêtés, beside their unfurnished twins already pinned by
// TestBaremeVintage_SpotChecks:
//
//   - Paris: arrêté IDF-2026-06-12-00003 of 12 June 2026, annexe 2,
//     "Locations meublées" columns (every one of the 2560 shipped Paris cells
//     was diffed against that annexe when the furnished pair was exposed: no
//     difference).
//   - Plaine Commune / Est Ensemble: the DRIHL référence-loyer KML "du 01 juin
//     2026", type "Meublée".
//   - Lyon / Villeurbanne: arrêté 69-2025-10-24-00001 (in force from
//     1 November 2025), annexe 1, "Locations meublées" columns.
func TestBaremeVintage_SpotChecksMeuble(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	find := func(entries []Entry, zoneID string, piece int, epoque string, maison bool) (Entry, bool) {
		for _, e := range entries {
			if (zoneID == "" || e.ZoneID == zoneID) && e.Piece == piece && e.Epoque == epoque && e.Meuble && e.Maison == maison {
				return e, true
			}
		}
		return Entry{}, false
	}
	cases := []struct {
		name             string
		entries          []Entry
		zoneID           string
		piece            int
		epoque           string
		maison           bool
		ref, min, majore float64
	}{
		{
			// Secteur 5, 1 pièce, > 1990: majoration unitaire 4.4.
			name:    "paris/Halles/1p/apres1990",
			entries: idx.LookupParis("01"), zoneID: "7510102",
			piece: 1, epoque: "Apres 1990", ref: 35.9, min: 25.1, majore: 43.1,
		},
		{
			// Secteur 13, 3 pièces, 1971-1990: majoration unitaire 2.3.
			name:    "paris/Gare/3p/1971-1990",
			entries: idx.LookupParis("13"), zoneID: "7511350",
			piece: 3, epoque: "1971-1990", ref: 19.0, min: 13.3, majore: 22.8,
		},
		{
			name:    "est_ensemble/308/Montreuil/1p/avant1946",
			entries: idx.LookupEPTZone(ZoneSourceEstEnsemble, "308"),
			piece:   1, epoque: "avant 1946", ref: 29.9, min: 20.9, majore: 35.9,
		},
		{
			name:    "plaine_commune/310/1p/avant1946",
			entries: idx.LookupEPTZone(ZoneSourcePlaineCommune, "310"),
			piece:   1, epoque: "avant 1946", ref: 26.1, min: 18.3, majore: 31.3,
		},
		{
			name:    "plaine_commune/311/maison/3p/1946-1970",
			entries: idx.LookupEPTZone(ZoneSourcePlaineCommune, "311"),
			piece:   3, epoque: "1946-1970", maison: true, ref: 16.9, min: 11.8, majore: 20.3,
		},
		{
			// IRIS 693810101 (Lyon 1er) lies in secteur 1.
			name:    "lyon/secteur1/2p/1946-1970",
			entries: idx.LookupLyonIRIS("693810101"),
			piece:   2, epoque: "1946-1970", ref: 15.4, min: 10.8, majore: 18.5,
		},
		{
			// IRIS 693810201 (Lyon 1er) lies in secteur 2; the grille's
			// highest meublé majoré.
			name:    "lyon/secteur2/1p/1991-2005",
			entries: idx.LookupLyonIRIS("693810201"),
			piece:   1, epoque: "1991-2005", ref: 21.9, min: 15.3, majore: 26.3,
		},
	}
	for _, c := range cases {
		if len(c.entries) == 0 {
			t.Errorf("%s: zone not loaded", c.name)
			continue
		}
		e, ok := find(c.entries, c.zoneID, c.piece, c.epoque, c.maison)
		if !ok {
			t.Errorf("%s: meublé cell not found in embedded barème", c.name)
			continue
		}
		if e.LoyerRefEURPerM2HC != c.ref || e.LoyerRefMinEURPerM2HC != c.min || e.LoyerRefMaxEURPerM2HC != c.majore {
			t.Errorf("%s: got ref=%.1f min=%.1f majoré=%.1f, want %.1f/%.1f/%.1f",
				c.name, e.LoyerRefEURPerM2HC, e.LoyerRefMinEURPerM2HC, e.LoyerRefMaxEURPerM2HC, c.ref, c.min, c.majore)
		}
	}
}

// A furnished letting used to be readable only against the unfurnished
// ceiling: cellFilter dropped every meublé cell. The Result now carries the
// meublé pair, collapsed over the same selection as the unfurnished one.
func TestQuery_FurnishedPair(t *testing.T) {
	t.Parallel()
	// Paris 1er, quartier Halles (IRIS 751010201), 1 room, built 2000: the
	// arrêté's secteur 5 cell, 37.8 unfurnished and 43.1 furnished majoré.
	r := query(t, gazetteer.Listing{INSEE: "75101", IRIS: "751010201", Rooms: intp(1), BuildYear: intp(2000)})
	if r.LoyerRefMajEURPerM2HC != 37.8 || r.LoyerRefEURPerM2HC != 31.5 {
		t.Errorf("unfurnished: maj %.2f ref %.2f, want 37.8 / 31.5", r.LoyerRefMajEURPerM2HC, r.LoyerRefEURPerM2HC)
	}
	if r.LoyerRefMajMeubleEURPerM2HC != 43.1 || r.LoyerRefMeubleEURPerM2HC != 35.9 {
		t.Errorf("furnished: maj %.2f ref %.2f, want 43.1 / 35.9", r.LoyerRefMajMeubleEURPerM2HC, r.LoyerRefMeubleEURPerM2HC)
	}
	if r.Evidence.NbMeubleCellsMatched != r.Evidence.NbCellsMatched {
		t.Errorf("NbMeubleCellsMatched = %d, NbCellsMatched = %d, want equal", r.Evidence.NbMeubleCellsMatched, r.Evidence.NbCellsMatched)
	}

	// Every zone system, collapsed or pinned, returns a furnished majoré above
	// the unfurnished one, from as many cells.
	for _, l := range []gazetteer.Listing{
		{INSEE: "75113"}, // multi-secteur arrondissement, no rooms, no year
		{INSEE: "69383", Rooms: intp(2)},
		{INSEE: "93048", Rooms: intp(3), BuildYear: intp(1960)}, // Montreuil, commune collapse
		{INSEE: "93066", Lat: new(48.9190), Lon: new(2.3540), Rooms: intp(3), PropertyType: gazetteer.PropertyHouse},
	} {
		if l.PropertyType == "" {
			l.PropertyType = gazetteer.PropertyApartment
		}
		r := queryAs(t, l)
		if r.IsEmpty() {
			t.Errorf("%s: empty", l.INSEE)
			continue
		}
		if r.LoyerRefMajMeubleEURPerM2HC <= r.LoyerRefMajEURPerM2HC {
			t.Errorf("%s: furnished majoré %.2f not above unfurnished %.2f", l.INSEE, r.LoyerRefMajMeubleEURPerM2HC, r.LoyerRefMajEURPerM2HC)
		}
		if r.Evidence.NbMeubleCellsMatched != r.Evidence.NbCellsMatched {
			t.Errorf("%s: %d meublé cells for %d unfurnished", l.INSEE, r.Evidence.NbMeubleCellsMatched, r.Evidence.NbCellsMatched)
		}
	}

	// Outside every zone, both pairs are zero.
	if r := query(t, gazetteer.Listing{INSEE: "75056"}); !r.IsEmpty() || r.LoyerRefMajMeubleEURPerM2HC != 0 {
		t.Errorf("outside: furnished majoré %.2f, want 0", r.LoyerRefMajMeubleEURPerM2HC)
	}
}

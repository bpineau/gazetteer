package encadrement

import "testing"

// TestBaremeVintage_NotStale is the legality guard: the embedded barème for
// every zone system must be drawn from an arrêté in force in 2025 or later, so
// a stale arrêté (the 2022 Plaine Commune / 2023 Est Ensemble the source once
// shipped) can never silently return to production. Bump the pinned vintages
// when refreshing, never this floor.
func TestBaremeVintage_NotStale(t *testing.T) {
	t.Parallel()
	const floor = 2025
	if parisYear < floor {
		t.Errorf("paris vintage %d < %d — stale arrêté", parisYear, floor)
	}
	if eptBaremeYear < floor {
		t.Errorf("EPT (Plaine Commune / Est Ensemble) vintage %d < %d — stale arrêté", eptBaremeYear, floor)
	}
}

// TestBaremeVintage_SpotChecks pins a handful of published (zone, pièces,
// époque) cells against the reference values of the arrêtés the guard claims
// to ship — so the constants above cannot drift away from the embedded data.
// Values transcribed from the DRIHL référence-loyer barème "du 01 juin 2026"
// (EPTs) and the Ville de Paris 2025 grille (loyers nus, HC, €/m²/mois).
func TestBaremeVintage_SpotChecks(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// zoneID "" matches any cell; a non-empty one pins a specific quartier
	// (Paris arrondissements carry several).
	find := func(entries []Entry, zoneID string, piece int, epoque string) (Entry, bool) {
		for _, e := range entries {
			if (zoneID == "" || e.ZoneID == zoneID) && e.Piece == piece && e.Epoque == epoque && !e.Meuble && !e.Maison {
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
		ref, min, majore float64 // reference / minoré / majoré, €/m²/mois HC
	}{
		{
			// DRIHL 2026-06-01, Est Ensemble zone 308 (Montreuil), 1 pièce,
			// avant 1946, non meublé.
			name:    "est_ensemble/308/Montreuil/1p/avant1946",
			entries: idx.LookupEPTZone(ZoneSourceEstEnsemble, "308"),
			piece:   1, epoque: "avant 1946", ref: 27.3, min: 19.1, majore: 32.8,
		},
		{
			// DRIHL 2026-06-01, Plaine Commune zone 310, 1 pièce, avant 1946,
			// non meublé.
			name:    "plaine_commune/310/1p/avant1946",
			entries: idx.LookupEPTZone(ZoneSourcePlaineCommune, "310"),
			piece:   1, epoque: "avant 1946", ref: 24.6, min: 17.2, majore: 29.5,
		},
		{
			// Ville de Paris 2025 grille, quartier Halles (code_grand_quartier
			// 7510102), 1 pièce, après 1990, non meublé.
			name:    "paris/Halles/1p/apres1990",
			entries: idx.LookupParis("01"), zoneID: "7510102",
			piece: 1, epoque: "Apres 1990", ref: 30.0, min: 21.0, majore: 36.0,
		},
	}
	for _, c := range cases {
		e, ok := find(c.entries, c.zoneID, c.piece, c.epoque)
		if !ok {
			t.Errorf("%s: cell not found in embedded barème", c.name)
			continue
		}
		if e.LoyerRefEURPerM2HC != c.ref || e.LoyerRefMinEURPerM2HC != c.min || e.LoyerRefMaxEURPerM2HC != c.majore {
			t.Errorf("%s: got ref=%.1f min=%.1f majoré=%.1f, want %.1f/%.1f/%.1f",
				c.name, e.LoyerRefEURPerM2HC, e.LoyerRefMinEURPerM2HC, e.LoyerRefMaxEURPerM2HC, c.ref, c.min, c.majore)
		}
	}
}

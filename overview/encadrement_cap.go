// Package overview provides a batch join of per-commune data from all embedded
// gazetteer sources, producing []CommuneOverview — the input to the prospection
// service. The join is embedded-only (no network, no CF).
package overview

import (
	"sort"

	"github.com/bpineau/gazetteer/sources/encadrement"
)

// Paris arrondissements: INSEE 75101-75120 ↔ arrondissements "01"-"20".
// Plaine Commune communes and their rent-control zones (from the embedded
// encadrement_plaine_commune_zones.json; hardcoded to avoid coupling to
// unexported Index internals).
// Est Ensemble communes and their zones.
//
// Source of truth: encadrement_plaine_commune_zones.json and
// encadrement_est_ensemble_zones.json (embedded in the encadrement package).
// Confirmed 2025-12 with the embedded artifacts.

// plaineCommuneCommunes is the set of Plaine Commune INSEE codes and their
// encadrement zone(s). Each commune belongs to exactly one zone, except
// Saint-Denis (93066) which spans two zones (311 and 312).
var plaineCommuneCommunes = map[string][]string{
	"93001": {"314"}, // Aubervilliers
	"93027": {"316"}, // L'Île-Saint-Denis
	"93031": {"315"}, // Épinay-sur-Seine
	"93039": {"312"}, // Stains
	"93059": {"317"}, // Pierrefitte-sur-Seine
	"93066": {"311", "312"}, // Saint-Denis
	"93070": {"310"}, // Saint-Ouen-sur-Seine
	"93072": {"318"}, // Villetaneuse
	"93079": {"316"}, // La Courneuve — zone 316 per the embedded data
}

// estEnsembleCommunes is the set of Est Ensemble INSEE codes and their
// encadrement zone(s).
var estEnsembleCommunes = map[string][]string{
	"93006": {"308"}, // Bagnolet
	"93008": {"315"}, // Bobigny — note: 315 here is EE zone, distinct from PC zone 315
	"93010": {"318"}, // Bondy
	"93045": {"307"}, // Les Lilas
	"93048": {"307", "308"}, // Montreuil
	"93053": {"311"}, // Noisy-le-Sec — note: 311 here is EE zone, distinct from PC zone 311
	"93055": {"308"}, // Pantin
	"93061": {"308"}, // Le Pré-Saint-Gervais
	"93063": {"313"}, // Romainville
}

// RepresentativeT2Majore returns a representative loyer de référence majoré
// for a T2 (2-room, unfurnished, non-house) apartment in the given commune, in
// EUR/m²/month HC. The value is the median LoyerRefMaxEURPerM2HC across all
// matching encadrement cells for the commune's zone(s).
//
// This is an approximation suitable for a prospection overview: the exact cap
// for a specific dwelling requires per-address zone resolution (époque,
// sous-zone within multi-zone communes). Use the /api/zone per-address endpoint
// for binding decisions.
//
// Returns (cap, true) for communes in Paris (75101–75120), Plaine Commune
// (9 communes), and Est Ensemble (9 communes). Returns (0, false) for all
// other communes.
func RepresentativeT2Majore(idx *encadrement.Index, insee string) (float64, bool) {
	if idx == nil || len(insee) != 5 {
		return 0, false
	}

	var entries []encadrement.Entry

	switch {
	case insee >= "75101" && insee <= "75120":
		// Paris: arrondissement is the 2-digit code at positions 3:5
		// (e.g. "75119" → "19"). The format matches LookupParis("01".."20").
		arr := insee[3:5]
		entries = idx.LookupParis(arr)

	default:
		if zones, ok := plaineCommuneCommunes[insee]; ok {
			for _, z := range zones {
				entries = append(entries, idx.LookupPlaineCommuneZone(z)...)
			}
		} else if zones, ok := estEnsembleCommunes[insee]; ok {
			for _, z := range zones {
				entries = append(entries, idx.LookupEstEnsembleZone(z)...)
			}
		} else {
			return 0, false
		}
	}

	// Filter to T2, non-meublé, non-maison.
	var vals []float64
	for _, e := range entries {
		if e.Piece == 2 && !e.Meuble && !e.Maison {
			if e.LoyerRefMaxEURPerM2HC > 0 {
				vals = append(vals, e.LoyerRefMaxEURPerM2HC)
			}
		}
	}
	if len(vals) == 0 {
		return 0, false
	}
	return medianFloat64(vals), true
}

// medianFloat64 returns the median of a non-empty slice (sorts in place).
func medianFloat64(vals []float64) float64 {
	sort.Float64s(vals)
	n := len(vals)
	if n%2 == 1 {
		return vals[n/2]
	}
	return (vals[n/2-1] + vals[n/2]) / 2.0
}

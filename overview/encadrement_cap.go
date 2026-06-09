package overview

import (
	"github.com/bpineau/gazetteer/helpers/stats"
	"github.com/bpineau/gazetteer/sources/encadrement"
)

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
// Returns (cap, true) for communes in Paris (75101–75120) and the EPT
// perimeters carried by the embedded encadrement zonage artifacts (Plaine
// Commune, Est Ensemble). Returns (0, false) for all other communes.
func RepresentativeT2Majore(idx *encadrement.Index, insee string) (float64, bool) {
	if idx == nil || len(insee) != 5 {
		return 0, false
	}

	var entries []encadrement.Entry

	if insee >= "75101" && insee <= "75120" {
		// Paris: arrondissement is the 2-digit code at positions 3:5
		// (e.g. "75119" → "19"). The format matches LookupParis("01".."20").
		entries = idx.LookupParis(insee[3:5])
	} else if zones, ept, ok := idx.ZonesForINSEE(insee); ok {
		for _, z := range zones {
			entries = append(entries, idx.LookupEPTZone(ept, z)...)
		}
	} else {
		return 0, false
	}

	// Filter to T2, non-meublé, non-maison.
	var vals []float64
	for _, e := range entries {
		if e.Piece == 2 && !e.Meuble && !e.Maison && e.LoyerRefMaxEURPerM2HC > 0 {
			vals = append(vals, e.LoyerRefMaxEURPerM2HC)
		}
	}
	if len(vals) == 0 {
		return 0, false
	}
	return stats.Median(vals), true
}

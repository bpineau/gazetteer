package overview

import (
	"time"

	"github.com/bpineau/gazetteer/helpers/stats"
	"github.com/bpineau/gazetteer/sources/encadrement"
)

// RepresentativeT2Majore returns a representative loyer de référence majoré
// for a T2 (2-room, unfurnished, non-house) apartment in the given commune, in
// EUR/m²/month HC, as in force on day (the zero time means today). The value
// is the median LoyerRefMaxEURPerM2HC across all matching encadrement cells for
// the commune's zone(s).
//
// This is an approximation suitable for a prospection overview: the exact cap
// for a specific dwelling requires per-address zone resolution (époque,
// sous-zone within multi-zone communes). Use the encadrement Source on the
// address for binding decisions.
//
// Returns (cap, true) for every commune the embedded encadrement artifacts
// cover (Paris 75101–75120, Lyon and Villeurbanne 69381–69389 and 69266, the
// two Seine-Saint-Denis EPT perimeters Plaine Commune and Est Ensemble) while
// their grille is in force on day (encadrement.RegulationOn). Returns
// (0, false) for all other communes, including the decreed rent-cap
// territories whose grille is not embedded (encadrement.UncoveredTerritory),
// which Build flags with CommuneOverview.EncadrementNotCovered instead of
// screening them as unregulated, and for every commune once the grille or
// the art. 140 experiment has lapsed on day.
func RepresentativeT2Majore(idx *encadrement.Index, insee string, day time.Time) (float64, bool) {
	entries, zoneSource := grilleCells(idx, insee)
	if zoneSource == "" || encadrement.RegulationOn(zoneSource, day) != encadrement.RegulationCapped {
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

// grilleCells returns the shipped encadrement cells covering a commune and
// the grille they come from (an encadrement.ZoneSource* constant), or
// (nil, "") for a commune no shipped grille covers.
func grilleCells(idx *encadrement.Index, insee string) ([]encadrement.Entry, string) {
	if idx == nil || len(insee) != 5 {
		return nil, ""
	}
	switch zones, ept, inEPT := idx.ZonesForINSEE(insee); {
	case insee >= "75101" && insee <= "75120":
		// Paris: arrondissement is the 2-digit code at positions 3:5
		// (e.g. "75119" → "19"). The format matches LookupParis("01".."20").
		return idx.LookupParis(insee[3:5]), encadrement.ZoneSourceParis
	case len(idx.LookupLyonInsee(insee)) > 0:
		// Lyon arrondissements and Villeurbanne, keyed by commune INSEE.
		return idx.LookupLyonInsee(insee), encadrement.ZoneSourceLyonVilleurbanne
	case inEPT:
		var entries []encadrement.Entry
		for _, z := range zones {
			entries = append(entries, idx.LookupEPTZone(ept, z)...)
		}
		return entries, ept
	default:
		return nil, ""
	}
}

// setEncadrement fills a row's encadrement fields as in force on day.
func setEncadrement(row *CommuneOverview, idx *encadrement.Index, insee string, day time.Time) {
	if cap, ok := RepresentativeT2Majore(idx, insee, day); ok {
		row.Encadree = true
		row.RentCapEURM2HC = &cap
		_, zs := grilleCells(idx, insee)
		row.RentCapHedged = encadrement.CapHedgedOn(zs, day)
		return
	}
	zoneSource := ""
	if _, zs := grilleCells(idx, insee); zs != "" {
		zoneSource = zs
	} else if _, _, ok := encadrement.UncoveredTerritory(insee); !ok {
		return // never regulated
	}
	switch encadrement.RegulationOn(zoneSource, day) {
	case encadrement.RegulationNotCovered:
		// Capped by a grille this module does not ship (an unshipped
		// territory, or a shipped grille past its own end).
		row.Encadree = true
		row.EncadrementNotCovered = true
	case encadrement.RegulationLapsed:
		row.EncadrementLapsed = true
	}
}

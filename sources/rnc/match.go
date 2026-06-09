package rnc

import (
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/fraddr"
	"github.com/bpineau/gazetteer/helpers/geodist"
)

const (
	// geoHighMeters: at or below this distance, with a street agreement,
	// the match is high-confidence (same building / entrance).
	geoHighMeters = 25.0
	// geoMaxMeters: the proximity gate. Beyond this, geo does not match.
	geoMaxMeters = 60.0
)

// normVoie reduces a raw address to its canonical street tokens (street-type
// markers like "rue"/"avenue" stripped, house number dropped). The SAME
// function is applied to both the upstream copro address (in transform) and
// the query address (here), so "20 r de gramont" and "Rue de Gramont" agree.
func normVoie(addr string) string {
	return strings.Join(fraddr.Parse(addr).StreetTokens, " ")
}

// match returns the best copro for the listing within its commune, with the
// match method, confidence and geo distance (metres).
func (idx *Index) match(l gazetteer.Listing) (*Entry, MatchMethod, string, float64) {
	insee := strings.TrimSpace(l.INSEE)
	if idx == nil || insee == "" {
		return nil, MatchNone, ConfidenceNone, 0
	}
	voie := normVoie(l.Address)
	cands := idx.ByInsee[insee]

	// 1) geo-proximity (primary), street as a confidence booster.
	if lat, lon, ok := l.Coords(); ok {
		bestIdx, bestD := -1, geoMaxMeters
		for _, i := range cands {
			e := &idx.Copros[i]
			if e.Lat == 0 && e.Lon == 0 {
				continue
			}
			d := geodist.MetersBetween(lat, lon, e.Lat, e.Lon)
			if d < bestD {
				bestIdx, bestD = i, d
			}
		}
		if bestIdx >= 0 {
			e := &idx.Copros[bestIdx]
			conf := ConfidenceMedium
			if bestD <= geoHighMeters && voieMatches(voie, e) {
				conf = ConfidenceHigh
			}
			return e, MatchGeoVoie, conf, bestD
		}
	}

	// 2) street-only within the commune, accepted only when unambiguous.
	if voie != "" {
		var hit *Entry
		n := 0
		for _, i := range cands {
			if voieMatches(voie, &idx.Copros[i]) {
				hit = &idx.Copros[i]
				n++
			}
		}
		if n == 1 {
			return hit, MatchVoie, ConfidenceLow, 0
		}
	}

	return nil, MatchNone, ConfidenceNone, 0
}

func voieMatches(voie string, e *Entry) bool {
	if voie == "" {
		return false
	}
	if e.VoieNorm != "" && (strings.Contains(e.VoieNorm, voie) || strings.Contains(voie, e.VoieNorm)) {
		return true
	}
	for _, vc := range e.VoiesComp {
		if vc != "" && (strings.Contains(vc, voie) || strings.Contains(voie, vc)) {
			return true
		}
	}
	return false
}

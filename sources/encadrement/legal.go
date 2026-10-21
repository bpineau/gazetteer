package encadrement

import (
	"fmt"
	"time"
)

// When the caps are law.
//
// Every grille this Source ships, and every territory it reports as
// regulated, rests on the EXPERIMENT of art. 140 of loi n° 2018-1021 (ELAN):
// "A titre expérimental et pour une durée de cinq ans à compter de la
// publication de la présente loi", a duration that loi n° 2022-217 (3DS) of
// 21 February 2022 raised to eight years. The law was published in the JO of
// 24 November 2018, so eight years run to 24 November 2026: the prefect's own
// arrêtés say so ("prend fin le 24 novembre 2026 à minuit", and each 2026
// grille is in force "jusqu'au 24 novembre 2026", see gridValidity). The
// Sénat's report n° 47 (2026-2027) dates the end "le 23 novembre", counting the
// day of publication; the arrêtés' reading is the one applied here, and it is
// also the one the delay rules of art. 641 and 642 of the code de procédure
// civile give (a delay in years ends on the same date, at midnight).
//
// STATUS as of 2026-10-21: NOT EXTENDED. The proposition de loi "Retrouver la
// confiance et l'équilibre dans les rapports locatifs" (AN texte adopté
// n° 196 of 11 December 2025, Sénat n° 217 (2025-2026)) would make the regime
// permanent; the Sénat's commission rewrote it on 14 October 2026 (texte de la
// commission n° 48 (2026-2027)) into a two-year extension, replacing "huit" by
// "dix" in art. 140, and the Sénat sitting on it was 21 October 2026. Nothing
// is adopted in identical terms by both chambers, let alone promulgated, so
// past elanLastDay the Source answers RegulationLapsed: no cap.
// Dossier: https://www.senat.fr/dossier-legislatif/ppl25-217.html
//
// WHEN AN EXTENSION IS PUBLISHED IN THE JO, change, in one commit:
//  1. elanLastDay to the new last day the law gives (a "dix ans" from the same
//     publication would read 24 November 2028; a permanent regime means
//     removing the lapse altogether), and this STATUS paragraph, with the
//     law's number and JO date;
//  2. gridValidity for every grille re-issued under it (the 2026 Paris and
//     93 arrêtés end on 24 November 2026 by their own terms, so until their
//     successors ship those zones read RegulationNotCovered, not capped);
//  3. sourceVersion, and the "Sunset" paragraph of docs/sources.md.
var elanLastDay = civilDate(2026, time.November, 24)

// validity is the period a shipped grille's arrêté puts it in force, both
// bounds inclusive. A zero until means the arrêté states no end: the grille
// stays in force until its successor, or until elanLastDay.
type validity struct {
	from, until time.Time
	arrete      string
}

// gridValidity is each shipped grille's own period, read off its arrêté:
//
//   - Paris: arrêté IDF-2026-06-12-00003 of 12 June 2026, art. 4, "à compter
//     du 1er juillet 2026 jusqu'au 24 novembre 2026"
//     (https://cdn.paris.fr/paris/2026/06/17/arrete-prefectoral-encadrement-des-loyers-12-06-2026-publie-n4wk.pdf);
//   - Est Ensemble and Plaine Commune: arrêtés IDF-2026-04-28-00032 and
//     IDF-2026-04-28-00033 of 28 April 2026, art. 4, "à compter du 1er juin
//     2026 jusqu'au 24 novembre 2026" (recueil des actes administratifs
//     spécial IDF-061-2026-04 of 30 April 2026; the DRIHL site's "au 31 mai
//     2027" label is not the arrêté's);
//   - Lyon / Villeurbanne: arrêté 69-2025-10-24-00001, art. 3, "à compter du
//     1er novembre 2025", no stated end.
var gridValidity = map[string]validity{
	ZoneSourceParis: {
		from: civilDate(2026, time.July, 1), until: civilDate(2026, time.November, 24),
		arrete: "arrêté IDF-2026-06-12-00003",
	},
	ZoneSourceEstEnsemble: {
		from: civilDate(2026, time.June, 1), until: civilDate(2026, time.November, 24),
		arrete: "arrêté IDF-2026-04-28-00032",
	},
	ZoneSourcePlaineCommune: {
		from: civilDate(2026, time.June, 1), until: civilDate(2026, time.November, 24),
		arrete: "arrêté IDF-2026-04-28-00033",
	},
	ZoneSourceLyonVilleurbanne: {
		from:   civilDate(2025, time.November, 1),
		arrete: "arrêté 69-2025-10-24-00001",
	},
}

// clock is the wall clock read when Listing.AsOf is zero. Tests pin it.
var clock = time.Now

// civilDate is the 00:00 UTC instant of a calendar day, the form every date
// of this file is compared in.
func civilDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dayOf reduces an instant to its calendar day (in its own location), so a
// Listing.AsOf stamped at any hour of 24 November still reads 24 November.
func dayOf(t time.Time) time.Time {
	return civilDate(t.Year(), t.Month(), t.Day())
}

// dated holds a reading to the date it is asked for: day is Listing.AsOf, or
// today. zoneSource names the shipped grille behind a capped reading ("" for a
// territory without one), and lastDay is the experiment's last day (Query
// passes elanLastDay).
//
//   - Past lastDay no art. 140 cap is law: any regulated reading becomes
//     RegulationLapsed, with no figure and ConfidenceNone.
//   - Past the grille's own end (only reachable once an extension moves
//     elanLastDay), the shipped cells are no longer the ones in force: no
//     figure, RegulationNotCovered.
//   - Before the grille's start, the cells in force were the previous
//     arrêté's, which this Source does not ship: the shipped cap is kept as
//     the nearest reading, at ConfidenceLow, with a Note.
func dated(r *Result, zoneSource string, day, lastDay time.Time) *Result {
	if r == nil || r.Regulation == "" {
		return r
	}
	if day.After(lastDay) {
		lapsed := &Result{
			Zone:       r.Zone,
			ZoneSource: r.ZoneSource,
			Territory:  r.Territory,
			Confidence: ConfidenceNone,
			Regulation: RegulationLapsed,
			Evidence:   r.Evidence,
		}
		lapsed.Evidence.Note = fmt.Sprintf("the art. 140 loi 2018-1021 rent-cap experiment ended on %s and no extension had been published in the JO when this Source was built; on %s no cap applies",
			lastDay.Format(time.DateOnly), day.Format(time.DateOnly))
		return lapsed
	}
	v, ok := gridValidity[zoneSource]
	if !ok || r.Regulation != RegulationCapped {
		return r
	}
	switch {
	case !v.until.IsZero() && day.After(v.until):
		expired := &Result{
			Zone:       r.Zone,
			ZoneSource: r.ZoneSource,
			Confidence: ConfidenceNone,
			Regulation: RegulationNotCovered,
			Evidence:   r.Evidence,
		}
		expired.Evidence.Note = fmt.Sprintf("the shipped grille (%s) was in force until %s; the one in force on %s is not embedded",
			v.arrete, v.until.Format(time.DateOnly), day.Format(time.DateOnly))
		return expired
	case day.Before(v.from):
		r.Confidence = ConfidenceLow
		r.Evidence.Note = fmt.Sprintf("the shipped grille (%s) is in force from %s; on %s the previous arrêté applied, whose cells this Source does not ship",
			v.arrete, v.from.Format(time.DateOnly), day.Format(time.DateOnly))
	}
	return r
}

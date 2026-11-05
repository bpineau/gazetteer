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
// STATUS as of 2026-11-05: EXTENSION ASSUMED, NOT YET LAW. The proposition
// de loi "Retrouver la confiance et l'équilibre dans les rapports locatifs"
// (AN texte adopté n° 196 of 11 December 2025, Sénat n° 217 (2025-2026)) was
// adopted by the Sénat on 21 October 2026 (texte n° 14 (2026-2027), 325 votes
// to 17) as a two-year extension: its article 1er replaces "huit" by "dix" in
// art. 140, for the territories already regulated. The AN commission des
// affaires économiques deposited its second-reading text (n° 3432-A0) on
// 4 November 2026 and the séance publique is scheduled on 18 November 2026; a
// vote "conforme" then promulgation before 24 November 2026 would make it law.
// By the owner's decision the Source assumes that outcome: elanLastDay is the
// extended last day ("dix ans" from the same publication), and a reading past
// elanPublishedLastDay, the last day of the law as published, is held at
// ConfidenceLow with an Evidence.Note saying the extension is assumed.
// Dossiers: https://www.senat.fr/dossier-legislatif/ppl25-217.html and
// https://www.assemblee-nationale.fr/17/dossiers/retrouver_confiance_equilibre_rapports_locatifs_17e.asp
//
// RE-CHECK after 18 November 2026, then in one commit:
//  1. extension published in the JO: elanPublishedLastDay to the law's last
//     day (elanLastDay, or no lapse at all for a permanent regime), this
//     STATUS with the law's number and JO date, and gridValidity for every
//     successor arrêté as its grille ships;
//  2. extension rejected or not law by 24 November 2026: elanLastDay back to
//     elanPublishedLastDay;
//  3. either way, sourceVersion and the "Sunset" paragraph of docs/sources.md.
var elanLastDay = civilDate(2028, time.November, 24)

// elanPublishedLastDay is the experiment's last day under the law as
// published in the JO: eight years from 24 November 2018. A reading dated
// past it rests on the assumed extension (see STATUS) and says so.
var elanPublishedLastDay = civilDate(2026, time.November, 24)

// assumedExtensionNote explains a reading that rests on the assumed
// extension of art. 140.
const assumedExtensionNote = "the art. 140 loi 2018-1021 experiment ends on 24 November 2026 under the law in the JO; this reading assumes its two-year extension (adopted by the Sénat on 21 October 2026, Assemblée nationale séance scheduled on 18 November 2026), which is not yet law"

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
//   - Past elanPublishedLastDay (and up to lastDay) the reading rests on the
//     assumed extension: kept, at ConfidenceLow at most, with a Note.
//   - Past the grille's own end, its successor's cells are not shipped: the
//     shipped cap is carried over as the nearest reading, at ConfidenceLow,
//     with a Note (a successor grille moves by a few percent; the figure is
//     not the one in force).
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
		lapsed.Evidence.Note = fmt.Sprintf("the art. 140 loi 2018-1021 rent-cap experiment ends on %s, the term this Source applies (see its legal status); on %s no cap applies",
			lastDay.Format(time.DateOnly), day.Format(time.DateOnly))
		return lapsed
	}
	if day.After(elanPublishedLastDay) {
		hedge(r, assumedExtensionNote)
	}
	v, ok := gridValidity[zoneSource]
	if !ok || r.Regulation != RegulationCapped {
		return r
	}
	switch {
	case !v.until.IsZero() && day.After(v.until):
		hedge(r, fmt.Sprintf("the shipped grille (%s) was in force until %s; its successor is not embedded, so its cells are carried over on %s",
			v.arrete, v.until.Format(time.DateOnly), day.Format(time.DateOnly)))
	case day.Before(v.from):
		hedge(r, fmt.Sprintf("the shipped grille (%s) is in force from %s; on %s the previous arrêté applied, whose cells this Source does not ship",
			v.arrete, v.from.Format(time.DateOnly), day.Format(time.DateOnly)))
	}
	return r
}

// hedge lowers r's confidence to ConfidenceLow (a ConfidenceNone reading,
// which carries no figure, stays none) and appends note to its Evidence.
func hedge(r *Result, note string) {
	if r.Confidence != ConfidenceNone {
		r.Confidence = ConfidenceLow
	}
	if r.Evidence.Note != "" {
		note = r.Evidence.Note + "; " + note
	}
	r.Evidence.Note = note
}

// RegulationOn is the batch form of the Source's date rule, for callers
// that read the grilles without a Listing (overview). zoneSource names the
// shipped grille (a ZoneSource* constant) behind a capped reading, or is
// empty for a decreed territory whose grille is not shipped
// (UncoveredTerritory). day is the reading date; the zero time means today.
//
// It returns the Regulation a Query would report on day: RegulationLapsed
// past the art. 140 experiment's last day, RegulationNotCovered past the
// shipped grille's own end (or for an unshipped grille), RegulationCapped
// otherwise. Before a grille's start the shipped cap is still the nearest
// reading, so it stays RegulationCapped, as the Source keeps it at
// ConfidenceLow.
func RegulationOn(zoneSource string, day time.Time) string {
	if day.IsZero() {
		day = clock()
	}
	reg := RegulationCapped
	if zoneSource == "" {
		reg = RegulationNotCovered
	}
	return dated(&Result{Regulation: reg}, zoneSource, dayOf(day), elanLastDay).Regulation
}

// CapHedgedOn reports whether a capped reading from the shipped grille
// zoneSource (a ZoneSource* constant) is hedged on day (the zero time means
// today): held at ConfidenceLow because it rests on the assumed extension of
// art. 140, or because day falls outside the grille's own arrêté period (its
// cells are then the nearest reading, not the law in force). Batch callers
// (overview) surface it next to the cap figure.
func CapHedgedOn(zoneSource string, day time.Time) bool {
	if day.IsZero() {
		day = clock()
	}
	r := dated(&Result{Regulation: RegulationCapped, Confidence: ConfidenceMedium}, zoneSource, dayOf(day), elanLastDay)
	return r.Regulation == RegulationCapped && r.Confidence == ConfidenceLow
}

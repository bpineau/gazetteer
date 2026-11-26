package encadrement

import (
	"fmt"
	"time"
)

// Article 140 I of loi 2018-1021 runs for ten years from publication in the
// JO on 24 November 2018, as amended by loi 2026-1072, art. 1 I A (JO of
// 22 November 2026). We include the anniversary, consistent with the
// prefectural orders at the former eight-year boundary. Under I bis a
// territory may request early withdrawal from all or part of its perimeter.
// https://www.legifrance.gouv.fr/jorf/id/JORFARTI000055119242
var elanLastDay = civilDate(2028, time.November, 24)

// validity is the period a shipped grille's arrêté puts it in force, both
// bounds inclusive. A zero until means the arrêté states no end: the grille
// stays in force until its successor, or until elanLastDay.
type validity struct {
	from, until time.Time
	arrete      string
}

// The three IDF orders of 23 November 2026 remove the original end date
// from article 4, leaving the rates and zones in force. The Rhône order
// of the same day extends the October 2026 rates through 31 October 2027.
// IDF: https://www.drihl.ile-de-france.developpement-durable.gouv.fr/IMG/pdf/prolongation_arrete_paris.pdf
// https://www.drihl.ile-de-france.developpement-durable.gouv.fr/IMG/pdf/prolongation_arrete_ee.pdf
// https://www.drihl.ile-de-france.developpement-durable.gouv.fr/IMG/pdf/prolongation_arrete_pco.pdf
// Rhône: https://www.rhone.gouv.fr/contenu/telechargement/71239/479733/file/20261123_encadrement_loyers_Lyon_Villeurbanne_global.pdf
var gridValidity = map[string]validity{
	ZoneSourceParis: {
		from:   civilDate(2026, time.July, 1),
		arrete: "arrêté IDF-2026-06-12-00003 as amended by IDF-2026-11-23-00002",
	},
	ZoneSourceEstEnsemble: {
		from:   civilDate(2026, time.June, 1),
		arrete: "arrêté IDF-2026-04-28-00032 as amended by IDF-2026-11-23-00003",
	},
	ZoneSourcePlaineCommune: {
		from:   civilDate(2026, time.June, 1),
		arrete: "arrêté IDF-2026-04-28-00033 as amended by IDF-2026-11-23-00004",
	},
	ZoneSourceLyonVilleurbanne: {
		from:   civilDate(2026, time.November, 1),
		until:  civilDate(2027, time.October, 31),
		arrete: "arrêté 69-2026-10-23-00004 as amended by 69-2026-11-23-00001",
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
		lapsed.Evidence.Note = fmt.Sprintf("the art. 140 loi 2018-1021 rent-cap experiment ends on %s. On %s this experiment no longer applies",
			lastDay.Format(time.DateOnly), day.Format(time.DateOnly))
		return lapsed
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
// past the experiment's last day, RegulationNotCovered for an unshipped
// grille, RegulationCapped for a shipped grille (see CapHedgedOn).
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
// today): held at ConfidenceLow because day falls outside the grille's own arrêté period (its
// cells are then the nearest reading, not the law in force). Batch callers
// (overview) surface it next to the cap figure.
func CapHedgedOn(zoneSource string, day time.Time) bool {
	if day.IsZero() {
		day = clock()
	}
	r := dated(&Result{Regulation: RegulationCapped, Confidence: ConfidenceMedium}, zoneSource, dayOf(day), elanLastDay)
	return r.Regulation == RegulationCapped && r.Confidence == ConfidenceLow
}

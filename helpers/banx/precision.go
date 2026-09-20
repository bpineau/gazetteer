package banx

import "strings"

// Precision is the granularity a geocoder matched at: what the returned
// coordinate is the position OF. BAN reports it as the feature's `type`
// property, and this type carries that string verbatim (lower-cased), so
// a value the API adds tomorrow survives the round-trip instead of being
// flattened to "unknown".
//
// It is the field that tells a town-centre pin from a doorstep. BAN
// answers EVERY query it can parse, so a nonexistent street in a real
// commune comes back as the commune itself, with a plausible coordinate
// and a low score:
//
//	{"score": 0.28, "type": "municipality", "label": "Montreuil"}
//
// Nothing about the coordinate says it is the mairie's. Only the type
// does.
//
// What each precision is good for:
//
//   - PrecisionHouseNumber: the address itself, metres away. Everything
//     works: a cadastral parcel lookup, a DPE match, a distance to the
//     nearest station, a point-in-polygon test against a QPV contour.
//   - PrecisionStreet: the street's centroid, tens to a few hundred
//     metres away depending on its length. Fine for anything measured at
//     neighbourhood scale (a QPV or a sensitive-zone perimeter, a transit
//     distance, an IRIS). NOT fine for a parcel: the parcel under a
//     street centroid belongs to whoever lives mid-street.
//   - PrecisionLocality: a place name (hameau, lieu-dit) with no street
//     granularity, typically rural and up to kilometres wide. Commune-
//     level readings only.
//   - PrecisionMunicipality: the commune's own centre. The coordinate
//     answers "which commune", nothing finer. Perfect for the commune-
//     keyed sources (price aggregates, taxe foncière, delinquance), and
//     wrong for every per-address reading, silently, because a commune
//     centre IS a real address with a real parcel and a real DPE.
//
// The zero value, PrecisionUnknown, means the geocoder did not say. It
// is NOT "coarse": a Listing assembled by hand, a test stub or a proxy
// that drops the field all land there, so a gate that refused it would
// refuse correct data. CoarserThan treats it as "cannot tell", mirroring
// how the INSEE cascade treats a missing Score.
type Precision string

// The four granularities BAN reports, coarsest first.
const (
	// PrecisionUnknown is "the geocoder did not report a type".
	PrecisionUnknown Precision = ""
	// PrecisionMunicipality is a commune centre.
	PrecisionMunicipality Precision = "municipality"
	// PrecisionLocality is a named place with no street granularity.
	PrecisionLocality Precision = "locality"
	// PrecisionStreet is a street centroid.
	PrecisionStreet Precision = "street"
	// PrecisionHouseNumber is the address itself.
	PrecisionHouseNumber Precision = "housenumber"
)

// ParsePrecision normalizes a raw geocoder type string (BAN's `type`
// property) into a Precision. Unrecognised values are kept verbatim
// (lower-cased, trimmed) and rank as unknown.
func ParsePrecision(s string) Precision {
	return Precision(strings.ToLower(strings.TrimSpace(s)))
}

// Rank orders the known granularities from 1 (municipality) to 4
// (housenumber). Anything unknown, including the zero value, ranks 0.
func (p Precision) Rank() int {
	switch p {
	case PrecisionMunicipality:
		return 1
	case PrecisionLocality:
		return 2
	case PrecisionStreet:
		return 3
	case PrecisionHouseNumber:
		return 4
	default:
		return 0
	}
}

// Known reports whether p is one of the four granularities this package
// orders. An unknown value can still be read and logged; it just cannot
// be compared.
func (p Precision) Known() bool { return p.Rank() > 0 }

// CoarserThan reports whether p is known to be coarser than min - the
// predicate a precision-sensitive caller gates on:
//
//	if prec.CoarserThan(banx.PrecisionStreet) { // refuse or downgrade }
//
// An unknown p (or an unknown min) answers false: "cannot tell" is not
// "too coarse", and refusing it would break every caller that assembles
// a Listing by hand. A caller that wants the opposite posture tests
// Known() itself.
func (p Precision) CoarserThan(min Precision) bool {
	r, m := p.Rank(), min.Rank()
	return r > 0 && m > 0 && r < m
}

// String returns the raw type string, so a Precision prints as BAN
// spelled it.
func (p Precision) String() string { return string(p) }

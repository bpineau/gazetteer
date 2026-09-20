package cadastre

import "strings"

// MapBaseURL is the Etalab cadastre viewer root used to build the
// per-parcel deeplink (style=ortho, parcelleId=<14-char id>).
// Variable so callers that want to point a UI at a different viewer
// (e.g. an internal mirror) can override it.
var MapBaseURL = "https://cadastre.data.gouv.fr/map"

// ParcelID composes the 14-char Etalab id from its four components:
//
//	INSEE   — 5 chars (commune or arrondissement code, "2A"/"2B" for Corsica).
//	Prefixe — 3 chars (usually "000"; the API exposes it as "com_abs").
//	Section — 2 chars (left-zero-padded if 1-char source).
//	Numero  — 4 chars (left-zero-padded if shorter).
//
// The three trailing components are padded on the LEFT, and truncated
// from the left when too long — the cadastre id semantics define the
// LAST N chars as the canonical payload (cf. dvfSectionCode in
// sources/dvf/cadastre.go, which applies the same rule).
//
// The INSEE is not padded at all: it is either five characters or it is
// not an INSEE, and ParcelID returns "" for anything else. A numeric
// code that lost its leading zero on the way in ("1053" for Ambérieux
// 01053) used to be padded on the RIGHT, producing "10530" — a
// well-formed code for a commune of the Aube, 350 km away, inside a
// 14-character id that looks perfect and a map deeplink that opens a
// stranger's parcel. Guessing which digit went missing is not this
// function's job, and the same call was made for postcodes in
// banx.deptMatchKey.
func ParcelID(insee, prefixe, section, numero string) string {
	if len(insee) != 5 {
		return ""
	}
	return insee + leftZeroPad(prefixe, 3) + leftZeroPad(section, 2) + leftZeroPad(numero, 4)
}

// MapURL composes the Etalab cadastre-viewer URL for the given
// 14-char parcel id. Returns "" on an empty id so callers can
// distinguish "no link" from a broken link.
func MapURL(id string) string {
	if id == "" {
		return ""
	}
	return MapBaseURL + "?style=ortho&parcelleId=" + id
}

// leftZeroPad pads s with leading '0' to reach exactly width. When s
// is longer than width, returns the LAST width chars — the cadastre
// id convention treats the trailing N chars as the canonical payload.
func leftZeroPad(s string, width int) string {
	if len(s) == width {
		return s
	}
	if len(s) > width {
		return s[len(s)-width:]
	}
	return strings.Repeat("0", width-len(s)) + s
}

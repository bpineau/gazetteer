package cadastre

import "strings"

// MapBaseURL is the Etalab cadastre viewer root used to build the
// per-parcel deeplink (style=ortho, parcelleId=<14-char id>).
// Variable so callers that want to point a UI at a different viewer
// (e.g. an internal mirror) can override it.
var MapBaseURL = "https://cadastre.data.gouv.fr/map"

// ParcelID composes the 14-char Etalab id from its four components:
//
//	INSEE   — 5 chars (commune or arrondissement code).
//	Prefixe — 3 chars (usually "000"; the API exposes it as "com_abs").
//	Section — 2 chars (left-zero-padded if 1-char source).
//	Numero  — 4 chars (left-zero-padded if shorter).
//
// Inputs that can't be padded to the right length (longer than the
// target field) are returned truncated to the right side — the
// cadastre id semantics define the LAST N chars as the canonical
// payload (cf. dvfSectionCode in sources/dvf/cadastre.go which applies
// the same rule).
//
// Empty INSEE returns the empty string — there is no meaningful id
// without a commune anchor.
func ParcelID(insee, prefixe, section, numero string) string {
	if insee == "" {
		return ""
	}
	return rightPad(insee, 5) + leftZeroPad(prefixe, 3) + leftZeroPad(section, 2) + leftZeroPad(numero, 4)
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

// rightPad pads s with trailing '0' to reach exactly width. When s is
// longer than width, returns the FIRST width chars. INSEE codes have
// fixed width by spec so this is a defensive no-op on the happy path.
func rightPad(s string, width int) string {
	if len(s) == width {
		return s
	}
	if len(s) > width {
		return s[:width]
	}
	return s + strings.Repeat("0", width-len(s))
}

package cadastre

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// BaseURL is the API Carto IGN cadastre-parcelle endpoint root.
// Variable (not const) so tests can swap it with httptest.NewServer.URL
// — same pattern as the other HTTP-backed Sources.
var BaseURL = "https://apicarto.ign.fr/api/cadastre/parcelle"

// BatiBaseURL is the cadastre.data.gouv.fr Etalab bundler root for
// per-commune building dumps. The full URL the Source actually hits is
// `<BatiBaseURL>/<INSEE>/geojson/batiments`. Variable so tests can
// swap it.
var BatiBaseURL = "https://cadastre.data.gouv.fr/bundler/cadastre-etalab/communes"

// LatLonDecimals is the precision (in decimal places) we encode in the
// GeoJSON Point. 6 decimals ≈ 10 cm — more than enough to land on the
// correct parcel and cheap to log.
const LatLonDecimals = 6

// ErrInsufficientFilter is returned by URLForLatLon when its inputs
// cannot produce a query API Carto will accept (NaN / out-of-range
// coords). The Source wraps this as gazetteer.ErrInsufficientInputs.
var ErrInsufficientFilter = errors.New("cadastre: insufficient filter inputs")

// URLForLatLon builds the API Carto parcelle URL.
//
// The endpoint accepts a single query parameter, `geom`, holding a
// URL-encoded GeoJSON Point. GeoJSON Position order is [lon, lat]
// (RFC 7946 §3.1.1), so the coordinates inside the encoded JSON go
// LON first, LAT second. Coordinates are formatted with up to
// LatLonDecimals decimals (trailing zeros stripped); NaN / Inf /
// out-of-range / (0,0) coords yield ErrInsufficientFilter.
func URLForLatLon(lat, lon float64) (string, error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return "", ErrInsufficientFilter
	}
	if lat < -90 || lat > 90 {
		return "", fmt.Errorf("%w: lat=%v out of range", ErrInsufficientFilter, lat)
	}
	if lon < -180 || lon > 180 {
		return "", fmt.Errorf("%w: lon=%v out of range", ErrInsufficientFilter, lon)
	}
	// (0,0) is a sentinel for "unset" coordinates (typical of a
	// geocoder that returned a zero struct). Refuse it — sending the
	// request would burn time on a guaranteed-empty FeatureCollection.
	if lat == 0 && lon == 0 {
		return "", fmt.Errorf("%w: lat=lon=0", ErrInsufficientFilter)
	}
	lonS := clampDecimals(strconv.FormatFloat(lon, 'f', -1, 64), LatLonDecimals)
	latS := clampDecimals(strconv.FormatFloat(lat, 'f', -1, 64), LatLonDecimals)
	// We embed the JSON literally rather than json.Marshal-ing a struct
	// — the shape is fixed and the literal keeps URLs grep-friendly in
	// logs (`coordinates":[2.35,48.85]` is the canonical form).
	geom := `{"type":"Point","coordinates":[` + lonS + `,` + latS + `]}`
	q := url.Values{}
	q.Set("geom", geom)
	return BaseURL + "?" + q.Encode(), nil
}

// BatimentsURLForINSEE builds the per-commune building dump URL.
// INSEE is the 5-digit commune code; for Paris / Lyon / Marseille
// callers MUST pass the arrondissement code (75104, 69383, 13208, ...)
// — that's what the cadastre.data.gouv.fr bundler indexes on, the
// parent INSEE returns an empty body. Inputs that are not 5
// digits-only are rejected with ErrInsufficientFilter.
func BatimentsURLForINSEE(insee string) (string, error) {
	if len(insee) != 5 {
		return "", fmt.Errorf("%w: insee=%q not 5 chars", ErrInsufficientFilter, insee)
	}
	for i := 0; i < len(insee); i++ {
		c := insee[i]
		// 2A / 2B (Corsica) — INSEE allows one letter at position 1.
		if c >= '0' && c <= '9' {
			continue
		}
		if i == 1 && (c == 'A' || c == 'B' || c == 'a' || c == 'b') {
			continue
		}
		return "", fmt.Errorf("%w: insee=%q has illegal char %q", ErrInsufficientFilter, insee, c)
	}
	return BatBaseURLPath(insee), nil
}

// BatBaseURLPath returns the building-dump URL for a validated INSEE.
// Exposed as a separate helper so applyBatiBaseURL has a clean
// substring to strip when rewriting the base.
func BatBaseURLPath(insee string) string {
	return BatiBaseURL + "/" + url.PathEscape(insee) + "/geojson/batiments"
}

// clampDecimals truncates a stringified float to at most n digits
// after the decimal point. Numbers without a decimal point are
// returned verbatim.
//
//	clampDecimals("48.8607421", 6) → "48.860742"
//	clampDecimals("48.86", 6)      → "48.86"
//	clampDecimals("48", 6)         → "48"
func clampDecimals(s string, n int) string {
	dot := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return s
	}
	maxLen := dot + 1 + n
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

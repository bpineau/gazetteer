package georisques

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// BaseURL is the Georisques rapport-risque endpoint root. Variable
// (not const) so tests can swap it with httptest.NewServer.URL — same
// pattern as the other HTTP-backed Sources.
var BaseURL = "https://georisques.gouv.fr/api/v1/resultats_rapport_risque"

// LatLonDecimals is the precision (in decimal places) we send to
// Georisques. The API tolerates 6 decimals (~10 cm); we cap at 6 to
// avoid float-formatting drift across runs.
const LatLonDecimals = 6

// ErrInsufficientFilter is returned by URLForLatLon when its inputs
// cannot produce a query the API will accept (out-of-range coords,
// NaN, …). The Source wraps this as gazetteer.ErrInsufficientInputs.
var ErrInsufficientFilter = errors.New("georisques: insufficient filter inputs")

// URLForLatLon builds the rapport-risque query URL.
//
// **CRITICAL**: the `latlon` parameter takes longitude **first**, then
// latitude — counter-intuitively, the parameter name suggests the
// opposite order. Inverting silently returns an empty report (not a
// 400). The TestURLForLatLon_OrderIsLonLat unit test locks this exact
// ordering, do not invert.
//
// Coordinates are formatted with up to LatLonDecimals decimals
// (trailing zeros stripped). NaN / Inf / coordinates outside the
// valid range yield ErrInsufficientFilter.
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
	// 0,0 is a sentinel for "unset" coordinates (typical of geocoder
	// failures that returned a zero struct). Refuse it to avoid
	// burning a quota slot on a guaranteed-empty rapport.
	if lat == 0 && lon == 0 {
		return "", fmt.Errorf("%w: lat=lon=0", ErrInsufficientFilter)
	}
	// Format with up to N decimals, strip trailing zeros — keeps URLs
	// readable in logs without changing semantics.
	latS := strconv.FormatFloat(lat, 'f', -1, 64)
	lonS := strconv.FormatFloat(lon, 'f', -1, 64)
	latS = clampDecimals(latS, LatLonDecimals)
	lonS = clampDecimals(lonS, LatLonDecimals)
	// Build the query manually: url.Values would percent-encode the
	// comma in `latlon=lon,lat`, which the API rejects.
	return BaseURL + "?latlon=" + lonS + "," + latS, nil
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
	for i := range len(s) {
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

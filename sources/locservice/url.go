package locservice

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/bpineau/gazetteer/gazetteer"
)

// BaseURL is the LocService tensiometre endpoint root. Variable (not
// const) so tests can swap it with httptest.NewServer.URL — same
// pattern as bdnb / georisques / bienici.
var BaseURL = "https://www.locservice.fr/tensiometre"

// ErrInsufficientFilter is returned by URLForINSEE when its inputs
// cannot produce a query the page will accept (empty INSEE). The
// Source wraps this as gazetteer.ErrInsufficientInputs.
var ErrInsufficientFilter = errors.New("locservice: insufficient filter inputs")

// URLForINSEE builds the tensiometre page URL for the given INSEE and
// optional logement keyword. `logement` is the LocService-side value
// (empty for "all types", or one of "chambre", "studio", "T2", "T3",
// "T4", "T5", "F3", "F4" — see NormalizeLogement for the "+" stripping
// rule).
func URLForINSEE(insee, logement string) (string, error) {
	if insee == "" {
		return "", fmt.Errorf("%w: empty INSEE", ErrInsufficientFilter)
	}
	if logement == "" {
		return fmt.Sprintf("%s/tensiometre-%s.html", BaseURL, url.PathEscape(insee)), nil
	}
	return fmt.Sprintf("%s/tensiometre-%s-%s.html", BaseURL,
		url.PathEscape(logement),
		url.PathEscape(insee),
	), nil
}

// NormalizeLogement strips a trailing "+" from a logement label,
// mimicking the LocService home-page JavaScript that does
// `logement.replace('+',”)` before forming the URL.
//
// Returns the empty string unchanged.
func NormalizeLogement(s string) string {
	if s == "" {
		return ""
	}
	if s[len(s)-1] == '+' {
		return s[:len(s)-1]
	}
	return s
}

// MapTypeToLogement maps a gazetteer.PropertyType + rooms count onto
// the LocService logement keyword.
//
// Returns the empty string ("Tous types") for unsupported combinations
// — caller then issues the commune-wide call.
//
// Conversion rules (mirror the a downstream consumer-side enricher pre-port):
//
//	Apartment + rooms=1  → "studio"
//	Apartment + rooms=2  → "T2"
//	Apartment + rooms=3  → "T3"
//	Apartment + rooms=4  → "T4"
//	Apartment + rooms≥5  → "T5"   (LocService "T5+" stripped)
//	Apartment + rooms=nil → ""
//	House     + rooms≤4 / nil → "F3"
//	House     + rooms≥5 → "F4"   (LocService "F4+" stripped)
//	others    → ""
func MapTypeToLogement(pt gazetteer.PropertyType, rooms *int) string {
	switch pt {
	case gazetteer.PropertyApartment:
		if rooms == nil {
			return ""
		}
		switch *rooms {
		case 1:
			return "studio"
		case 2:
			return "T2"
		case 3:
			return "T3"
		case 4:
			return "T4"
		default:
			if *rooms >= 5 {
				return "T5" // LocService: "T5+" with the '+' stripped
			}
			return ""
		}
	case gazetteer.PropertyHouse:
		if rooms == nil || *rooms <= 4 {
			return "F3"
		}
		return "F4" // LocService: "F4+" with the '+' stripped
	default:
		return ""
	}
}

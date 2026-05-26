package dvf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// CadastreSectionsBaseURL is the cadastre-etalab JSON endpoint that lists
// every cadastral section feature for a given INSEE commune. Exposed as a
// var for tests.
//
// Schema (excerpt):
//
//	{
//	  "type": "FeatureCollection",
//	  "features": [
//	    {"id": "930720000A",
//	     "properties": {"commune": "93072", "prefixe": "000", "code": "A"}},
//	    ...
//	  ]
//	}
//
// This is the same endpoint that app.dvf.etalab.gouv.fr's webapp uses to
// populate its sections layer (cf. js/data.js → getCadastreLayer). Using
// it here lets us know the *exact* list of sections that exist for a
// commune without brute-forcing the 26×26 namespace — critical for
// communes whose section codes are 1-letter (e.g. Stains 93072 = A,B,C…)
// which the existing 000AA..000ZZ walker can never discover.
var CadastreSectionsBaseURL = "https://cadastre.data.gouv.fr/bundler/cadastre-etalab/communes"

// ErrCadastreCommuneNotFound is returned when the cadastre-etalab API has
// no sections file for the given INSEE (typically the umbrella codes
// `75056`, `13055`, `69123` of Paris/Marseille/Lyon — our pipeline never
// resolves to those, but defensive nonetheless).
var ErrCadastreCommuneNotFound = errors.New("dvf: cadastre commune not found")

// cadastreFeatureCollection is the GeoJSON envelope returned by
// cadastre.data.gouv.fr. We only model the few fields we need.
type cadastreFeatureCollection struct {
	Features []cadastreFeature `json:"features"`
}

type cadastreFeature struct {
	Properties cadastreSectionProps `json:"properties"`
}

type cadastreSectionProps struct {
	Commune string `json:"commune"`
	Prefixe string `json:"prefixe"`
	Code    string `json:"code"`
}

// FetchCadastreSections returns the list of cadastral section codes for
// commune `insee`, formatted for the DVF Etalab API path component (5
// chars total: `prefixe` (always "000") + left-padded code, e.g. "000AA"
// or "0000A"). On a 404 from the cadastre API, returns
// ErrCadastreCommuneNotFound.
//
// The DVF API path is `/api/mutations3/{insee}/{section}` where section
// is exactly the value `idSectionToCode` extracts in the DVF webapp js:
// `featureID.substr(5, 5)`. A featureID like "930720000A" yields "0000A";
// "751190000AA" yields "000AA". So the rule is:
//
//	dvfSection = "0" * (5 - len(prefixe) - len(code)) + prefixe + code
//
// Equivalent to right-aligning `prefixe + code` in a 5-char field.
func FetchCadastreSections(ctx context.Context, http *httpx.Client, insee string) ([]string, error) {
	if http == nil {
		return nil, errors.New("dvf: nil http client")
	}
	if insee == "" {
		return nil, errors.New("dvf: empty insee")
	}
	u := fmt.Sprintf("%s/%s/geojson/sections",
		CadastreSectionsBaseURL,
		url.PathEscape(insee),
	)
	body, _, err := http.GetBytes(ctx, u, nil)
	if err != nil {
		if herr, ok := errors.AsType[*httpx.ErrHTTP](err); ok && herr.Status == 404 {
			return nil, ErrCadastreCommuneNotFound
		}
		return nil, fmt.Errorf("dvf: cadastre GET %s: %w", insee, err)
	}
	var fc cadastreFeatureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("dvf: cadastre decode %s: %w", insee, err)
	}
	out := make([]string, 0, len(fc.Features))
	seen := make(map[string]struct{}, len(fc.Features))
	for _, f := range fc.Features {
		// Defensive: skip features that don't actually belong to this
		// commune. Cadastre data is occasionally noisy near commune
		// borders.
		if f.Properties.Commune != insee {
			continue
		}
		dvfCode := dvfSectionCode(f.Properties.Prefixe, f.Properties.Code)
		if dvfCode == "" {
			continue
		}
		if _, dup := seen[dvfCode]; dup {
			continue
		}
		seen[dvfCode] = struct{}{}
		out = append(out, dvfCode)
	}
	return out, nil
}

// dvfSectionCode formats a (prefixe, code) pair as the 5-char string the
// DVF Etalab API expects. Returns "" for malformed inputs.
//
// Examples (real-world):
//
//	("000", "AA") → "000AA"   (typical Paris arrondissement)
//	("000", "A")  → "0000A"   (typical small commune, e.g. Stains 93072)
//	("050", "AB") → "050AB"   (rare — non-default prefix)
func dvfSectionCode(prefixe, code string) string {
	if prefixe == "" || code == "" {
		return ""
	}
	combined := prefixe + code
	if len(combined) >= 5 {
		// Already wide enough — the API path uses the last 5 chars of
		// the cadastre id (cf. dvf-app js/index.js idSectionToCode).
		return combined[len(combined)-5:]
	}
	// Left-pad with '0' to reach 5 chars.
	pad := 5 - len(combined)
	zeros := []byte{'0', '0', '0', '0', '0'}
	return string(zeros[:pad]) + combined
}

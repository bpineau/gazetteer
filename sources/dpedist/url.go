package dpedist

import (
	"errors"
	"net/url"
	"strings"
)

// URLForINSEE builds the data-fair `values_agg` URL that aggregates
// the ADEME DPE-existants dataset by `etiquette_dpe` for the given
// commune INSEE. The Source GETs this URL once per Listing.
//
// `base` is the dataset's values_agg root (typically DefaultBaseURL or
// a httptest server URL). Returns an error when base is malformed or
// insee is empty.
func URLForINSEE(base, insee string) (string, error) {
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return "", errors.New("dpedist: empty INSEE")
	}
	if base == "" {
		return "", errors.New("dpedist: empty base URL")
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("field", "etiquette_dpe")
	// data-fair uses lucene-style query strings on `qs`; quoting the
	// INSEE keeps zip-like leading zeros intact.
	q.Set("qs", `code_insee_ban:"`+insee+`"`)
	// size=0 returns the bucket totals without any sample rows.
	q.Set("size", "0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

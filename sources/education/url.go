package education

import (
	"errors"
	"net/url"
	"strings"
)

// URLForINSEE builds the Opendatasoft v2.1 records URL that filters
// the Annuaire de l'Éducation Nationale on the given commune INSEE,
// limited to open establishments, and groups by type_etablissement so
// we get the count breakdown in a single request.
//
// `base` is the dataset records root (typically DefaultBaseURL or a
// httptest server URL). Returns an error when base is malformed or
// insee is empty.
func URLForINSEE(base, insee string) (string, error) {
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return "", errors.New("education: empty INSEE")
	}
	if base == "" {
		return "", errors.New("education: empty base URL")
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("where", "code_commune=\""+insee+"\" and etat=\"OUVERT\"")
	q.Set("select", "type_etablissement,count(*)")
	q.Set("group_by", "type_etablissement")
	// Pre-sized to avoid pagination — there are at most a handful of
	// distinct type_etablissement buckets (≤ 8 today).
	q.Set("limit", "20")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

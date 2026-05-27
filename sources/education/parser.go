package education

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// apiResponse mirrors the Opendatasoft v2.1 records envelope this
// Source consumes. Only the fields used downstream are unmarshalled.
type apiResponse struct {
	TotalCount int `json:"total_count"`
	Results    []struct {
		// TypeEtablissement may be null in the upstream payload — the
		// JSON decoder folds null onto the zero-value empty string.
		TypeEtablissement string `json:"type_etablissement"`
		Count             int    `json:"count(*)"`
	} `json:"results"`
}

// Parse turns the upstream JSON body into a Result. The caller stamps
// Confidence + Evidence on top. Returns a non-nil *Result on success
// (possibly with NbTotal == 0) ; returns an error when the body
// could not be decoded.
func Parse(body []byte) (*Result, error) {
	if len(body) == 0 {
		return nil, errors.New("education: empty body")
	}
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("education: decode json: %w", err)
	}

	out := &Result{}
	for _, row := range resp.Results {
		switch foldType(row.TypeEtablissement) {
		case TypeEcole:
			out.NbEcole += row.Count
		case TypeCollege:
			out.NbCollege += row.Count
		case TypeLycee:
			out.NbLycee += row.Count
		case TypeMedicoSoc:
			out.NbMedicoSocial += row.Count
		default:
			out.NbOther += row.Count
		}
	}
	out.NbTotal = out.NbEcole + out.NbCollege + out.NbLycee +
		out.NbMedicoSocial + out.NbOther
	return out, nil
}

// foldType normalises the upstream `type_etablissement` strings onto
// the package's Type enum. Unknown / null values map to TypeOther.
func foldType(s string) Type {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ecole", "école":
		return TypeEcole
	case "college", "collège":
		return TypeCollege
	case "lycee", "lycée":
		return TypeLycee
	case "medico-social", "médico-social":
		return TypeMedicoSoc
	default:
		return TypeOther
	}
}

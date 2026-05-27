package education

// Result is the typed payload returned by Source.Query. A per-commune
// count of OPEN establishments broken down by Type, plus the total.
type Result struct {
	// NbEcole is the count of "Ecole" rows (maternelle + élémentaire +
	// primaire — the dataset does not split them on the type field).
	NbEcole int `json:"nb_ecole"`

	// NbCollege is the count of "Collège" rows.
	NbCollege int `json:"nb_college"`

	// NbLycee is the count of "Lycée" rows.
	NbLycee int `json:"nb_lycee"`

	// NbMedicoSocial is the count of "Médico-social" establishments
	// (IME, ITEP, etc.) — included for completeness; usually low
	// rental-investor relevance.
	NbMedicoSocial int `json:"nb_medico_social,omitempty"`

	// NbOther is the count of everything else (Service Administratif
	// + null type).
	NbOther int `json:"nb_other,omitempty"`

	// NbTotal is the sum of the buckets above. Surfaced so the UI can
	// render "N établissements" without summing client-side.
	NbTotal int `json:"nb_total"`

	// Confidence is ConfidenceHigh when NbTotal > 0, ConfidenceNone
	// otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on.
	INSEE string `json:"insee"`

	// URL is the upstream URL the Source actually hit. Useful for
	// audit + dashboard "open in browser" affordances.
	URL string `json:"url,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when no
// establishments were found in the commune.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.NbTotal == 0
}

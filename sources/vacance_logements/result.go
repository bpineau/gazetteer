package vacance_logements

// Result is the typed payload returned by Source.Query.
type Result struct {
	// VacancyRate is the share of LOGVAC over LOG in the commune,
	// expressed as a percentage in [0, 100].
	VacancyRate float64 `json:"vacancy_rate"`

	// VacantCount is P21_LOGVAC — the number of vacant logements
	// observed in the census.
	VacantCount int `json:"vacant_count,omitempty"`

	// TotalLogements is P21_LOG — the total number of logements in the
	// commune, all categories combined.
	TotalLogements int `json:"total_logements,omitempty"`

	// ResidencesPrincipales is P21_RP, exposed for callers that want to
	// cross-reference with other commune-level signals.
	ResidencesPrincipales int `json:"residences_principales,omitempty"`

	// ResidencesSecondaires is P21_RSECOCC (résidences secondaires +
	// logements occasionnels), useful to distinguish a touristic
	// "vacance" peak from a structural one.
	ResidencesSecondaires int `json:"residences_secondaires,omitempty"`

	// Tier is the distribution-relative bucket (tendu / normal / élevé
	// / déprise). TierUnknown when the commune is missing from the
	// dataset.
	Tier Tier `json:"tier,omitempty"`

	// Confidence is "high" when the commune was located in the dataset,
	// ConfidenceNone otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on. The
	// vacance_logements source does NOT fold arrondissements — Paris,
	// Lyon and Marseille arrondissements carry their own rows.
	INSEE string `json:"insee"`

	// DataYear is the census vintage of the upstream dataset.
	DataYear int `json:"data_year,omitempty"`

	// RowCountCommunes is the total number of communes (incl.
	// arrondissements) in the embedded dataset.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was missing from the embedded crosswalk — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Confidence == ConfidenceNone
}

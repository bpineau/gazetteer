package cartofriches

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Result is the typed payload returned by Source.Query.
type Result struct {
	// SiteCount is the total number of Cartofriches-referenced sites
	// in the commune.
	SiteCount int `json:"site_count"`

	// TotalSurfaceM2 is the cumulative unite foncière surface across
	// all sites (square metres). Zero when none of the sites publish
	// a surface.
	TotalSurfaceM2 int `json:"total_surface_m2,omitempty"`

	// ByType breaks the SiteCount down by site_type
	// (e.g. "friche industrielle", "friche d'habitat", "friche
	// commerciale"). Map is keyed by the upstream French label.
	ByType map[string]int `json:"by_type,omitempty"`

	// ByStatus breaks the SiteCount down by site_statut
	// (e.g. "friche avec projet", "friche sans projet",
	// "site reconverti").
	ByStatus map[string]int `json:"by_status,omitempty"`

	// Confidence is "high" when the commune was found in the dataset,
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
	// INSEE is the 5-digit commune code the Source filtered on.
	INSEE string `json:"insee"`

	// CommuneLabel is the human-readable commune name from the source
	// file. Useful for logging / diagnostics; not always populated.
	CommuneLabel string `json:"commune_label,omitempty"`

	// RowCountCommunes is the total number of communes hosting at
	// least one site in the dataset.
	RowCountCommunes int `json:"row_count_communes,omitempty"`

	// RowCountSites is the total number of sites in the dataset.
	RowCountSites int `json:"row_count_sites,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune hosts no referenced site — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SiteCount == 0
}

package iris

// Confidence value returned in Result.Confidence. A resolved IRIS is an exact
// point-in-polygon match, so a populated reading is high confidence.
const ConfidenceHigh = "high"

// Result is the typed payload returned by Source.Query: the IRIS containing the
// listing's coordinates.
type Result struct {
	// CodeIRIS is the 9-digit INSEE IRIS code (e.g. "751104201"). Empty when the
	// point falls outside the covered perimeter.
	CodeIRIS string `json:"code_iris,omitempty"`

	// NomIRIS is the IRIS name (often the neighbourhood; the commune name for
	// undivided communes).
	NomIRIS string `json:"nom_iris,omitempty"`

	// TypIRIS is the IRIS type: "H" (habitat), "A" (activité), "D" (divers),
	// "Z" (commune non découpée).
	TypIRIS string `json:"typ_iris,omitempty"`

	// Confidence is "high" on a match, empty otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// ListingLat / ListingLon are the input coordinates.
	ListingLat float64 `json:"listing_lat"`
	ListingLon float64 `json:"listing_lon"`

	// Source is "listing" when the IRIS was taken from a pre-resolved
	// Listing.IRIS, or "geometry" when resolved by point-in-polygon here.
	Source string `json:"source,omitempty"`

	// PerimeterIRIS is the number of IRIS in the embedded perimeter.
	PerimeterIRIS int `json:"perimeter_iris,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no IRIS contains the
// listing (outside the covered perimeter).
func (r *Result) IsEmpty() bool {
	return r == nil || r.CodeIRIS == ""
}

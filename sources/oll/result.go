package oll

import (
	"fmt"
	"math"

	"github.com/bpineau/gazetteer/appraisal"
)

// Confidence values returned in Result.Confidence. Stable strings so downstream
// consumers can match on them without importing this package. Confidence tracks
// the cell's sample size (nombre_observations).
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
	ConfidenceNone   = ""
)

// Result is the typed payload returned by Source.Query: the observed market
// rent for the listing's OLL zone and rooms bucket.
//
// Envelope-only fields are the framework's responsibility (see gazetteer.Result).
type Result struct {
	// ObservedMedianEURPerM2 is the observed median rent, €/m²/month, HORS
	// CHARGES (OLL's headline indicator is the loyer de base hors charges;
	// see RentEstimate for why this matches encadrement). Zero when no cell
	// matched.
	ObservedMedianEURPerM2 float64 `json:"observed_median_eur_per_m2"`

	// ObservedQ1EURPerM2 / ObservedQ3EURPerM2 are the first and third quartiles
	// of the same cell — the inter-quartile band around the median.
	ObservedQ1EURPerM2 float64 `json:"observed_q1_eur_per_m2,omitempty"`
	ObservedQ3EURPerM2 float64 `json:"observed_q3_eur_per_m2,omitempty"`

	// AvgSurfaceM2 is the mean surface of the dwellings in the cell — useful to
	// sanity-check the rooms bucket. Zero when unknown.
	AvgSurfaceM2 float64 `json:"avg_surface_m2,omitempty"`

	// SampleSize is the number of observed leases behind the cell
	// (nombre_observations). Drives Confidence.
	SampleSize int `json:"sample_size"`

	// Zone is the OLL zone label the listing resolved to ("Zone 5"). Empty when
	// no match.
	Zone string `json:"zone,omitempty"`

	// Agglo is the human-readable observatory perimeter name. Empty when no match.
	Agglo string `json:"agglo,omitempty"`

	// Pieces is the rooms bucket used (1..4; 4 means "4 et plus"). 0 means the
	// zone-level all-sizes aggregate was used (the listing had no room count, or
	// its bucket had no observed cell).
	Pieces int `json:"pieces,omitempty"`

	// Confidence is "high"/"medium"/"low" by sample size on a match,
	// empty otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that produced a
// Result. Sidecar — travels in-process from Source.Query to the consumer.
type Evidence struct {
	// INSEE is the commune code the Source resolved.
	INSEE string `json:"insee,omitempty"`

	// AggloCode is the OLL agglomeration code (e.g. "L7502").
	AggloCode string `json:"agglo_code,omitempty"`

	// ZoneID is the resolved zone number inside the agglomeration (e.g. "5").
	ZoneID string `json:"zone_id,omitempty"`

	// Year is the observatory vintage the snapshot carries.
	Year int `json:"year,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no OLL cell matched the
// listing (outside the covered perimeter, or no observed rent for the bucket).
func (r *Result) IsEmpty() bool {
	return r == nil || r.ObservedMedianEURPerM2 <= 0
}

// RentEstimate satisfies appraisal.RentEstimator. OLL is an observed market
// rent (not a legal cap), so it carries no Bracket — it contributes a
// representative market reading to the consolidated synthesis.
//
// Basis: OLL publishes the median rent HORS CHARGES per m² of habitable surface
// (the OLL methodology's headline indicator), the same basis as encadrement
// (loyer de référence HC) and as carteloyers once it applies its CC→HC factor —
// so the three blend into appraisal.RentValue on a single hors-charges basis.
func (r *Result) RentEstimate() appraisal.RentEstimate {
	if r == nil || r.ObservedMedianEURPerM2 <= 0 {
		return appraisal.RentEstimate{}
	}
	return appraisal.RentEstimate{
		EurPerM2Cents: int64(math.Round(r.ObservedMedianEURPerM2 * 100)),
		Confidence:    mapConfidence(r.Confidence),
		Method:        fmt.Sprintf("oll_%s_z%s_p%d", r.Evidence.AggloCode, r.Evidence.ZoneID, r.Pieces),
	}
}

// mapConfidence translates OLL's confidence strings to the appraisal enum.
func mapConfidence(s string) appraisal.Confidence {
	switch s {
	case ConfidenceHigh:
		return appraisal.ConfidenceHigh
	case ConfidenceMedium:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}

// confidenceForN maps a sample size to a confidence tier. The thresholds match
// the OLL methodology's rule of thumb for cell reliability.
func confidenceForN(n int) string {
	switch {
	case n >= 50:
		return ConfidenceHigh
	case n >= 20:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

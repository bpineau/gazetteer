// Package appraisal synthesises weighted estimates (price-per-m², rent
// value, hazard profile) from a gazetteer.Dossier via small contribution
// interfaces.
//
// A Source's typed Result implements appraisal.PriceEstimator iff it
// produces a price-per-m² estimate; appraisal.PricePerM2 iterates the
// Dossier, picks up everything that implements the interface, applies
// configurable weights, and returns a consolidated estimate with
// breakdown.
//
// See doc/gazetteer/2026-05-25-extraction-design.md §5 for the design.
package appraisal

// Package appraisal synthesises consolidated views over a
// gazetteer.Dossier by combining the contributions of every Source
// whose typed Result satisfies one of the three contribution
// interfaces.
//
// Three views ship out of the box:
//
//   - PricePerM2 — weighted-mean price-per-m² with MAD outlier
//     rejection. Sources contribute via PriceEstimator.
//   - RentValue  — weighted-mean rent-per-m²/month with a Bracket
//     identifier for regulated zones. Sources contribute via
//     RentEstimator.
//   - HazardProfile — set-union of natural and industrial hazard
//     identifiers across every contributor. Sources contribute via
//     HazardReporter.
//
// All three are pure functions over a Dossier: pass the value
// produced by gazetteer.Client.Collect and read the consolidated
// output. Per-Source weights come from the DefaultPriceWeights /
// DefaultRentWeights tables, an override map in Options, or
// Options.DefaultWeight as a final fallback. Inputs lists the
// per-Source breakdown (including outlier-excluded entries) so
// callers can render attribution UIs.
//
// Example:
//
//	dossier := client.Collect(ctx, listing)
//	price   := appraisal.PricePerM2(dossier)
//	rent    := appraisal.RentValue(dossier)
//	hazard  := appraisal.HazardProfile(dossier)
//
// Confidence is one of ConfidenceLow / Medium / High; the String()
// method returns the stable snake_case identifier suitable for logs
// and metrics labels.
package appraisal

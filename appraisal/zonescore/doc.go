// Package zonescore turns a gazetteer.Dossier into a single, explainable
// rental-investment score for a zone, tuned for a YIELD-first investor.
//
// It is a terminal consumer: it reads the typed Results other sources produced
// (via gazetteer.Get) and the consolidated appraisal views, normalises each into
// a 0–100 axis, and combines the axes by weight into a 0–100 Composite. The
// rendement (gross-yield) axis dominates; the rest temper it with the factors
// that protect a realised yield — letting (tension, vacancy), tenant
// reliability (income, employment), safety, the net-yield drag of the property
// tax, and access / livability.
//
// Every Axis carries its Value, Weight, contributing Sources and a human Reason,
// so the Composite is auditable rather than a black box. Missing sources degrade
// gracefully: an absent axis is dropped and the remaining weights are
// renormalised, with the Confidence reflecting how much of the intended weight
// (and the dominant yield signal) was actually present.
//
// The default weights encode a yield-first thesis; override them via Options for
// a different profile.
//
// This package lives under appraisal but is NOT imported by it, so it may import
// the concrete sources/* packages it scores without an import cycle.
package zonescore

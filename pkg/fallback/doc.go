// Package fallback formalises the "ladder of fallback tiers" pattern that
// turns up every time a consumer fetches a derived value from an upstream
// with multiple plausible strategies.
//
// # Why
//
// Each enricher in this project fetches a €/m² estimate (or a geocoded
// location, or a building identifier) for an address. The primary
// strategy can fail in many ways: DataDome captcha, 5xx, request
// timeout, upstream parser drift, or — the common case — a successful
// response with a sample too small to be meaningful (e.g. a DVF query
// on a cadastral section returning zero apartments).
//
// Historically each enricher carried its own ad-hoc try/catch chain:
// hard to read, hard to observe, hard to test. A typed []Tier per
// consumer gives one place to read the priority order, one structured
// slog event per attempt, and one easy unit-test surface (assert tier
// order, assert SkipOn fires on the empty-sample case).
//
// # Mental model
//
// A Tier is a strategy. Walk runs each Tier in order; the first one
// that returns nil error AND produces an Output that does not satisfy
// SkipOn wins. Walk does NOT retry within a tier — per-call retries
// belong inside the Try function (typically delegated to pkg/httpx
// whose retry middleware handles it for you).
//
// # When to reach down a layer
//
// The Input struct is intentionally narrow (address-shaped). When a
// tier needs extras — a cadastral section, an INSEE code, a polygon —
// capture them in the closure that builds the Try function. The walker
// is just orchestration; it is not a typed function over arbitrary
// shapes.
//
// When the upstream value isn't an address-shaped €/m² number, wrap
// fallback.Walk in a tiny adapter that maps your native output to/from
// fallback.Output. This is what every enricher in
// internal/core/enrich/<name>/fallback.go does. The 5-line adapter
// preserves the free slog observability without a generic Output type
// blowing up the surface area.
//
// # Stability
//
// Public API is frozen for the duration of the library-extraction
// project (doc/specs/library_extraction_plan.md §2.6).
package fallback

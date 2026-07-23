package gazetteer

import "errors"

// Sentinel errors that Sources return (possibly wrapped) to signal what
// happened. The Client translates these to Result.Status per the table
// documented in source.go.
var (
	// ErrInsufficientInputs signals that the Listing lacks data the
	// Source needs to run (e.g. no surface_m2 for a price/m² source).
	// Treated as a transient blocker by stateful callers — retried when
	// the input set changes.
	ErrInsufficientInputs = errors.New("gazetteer: insufficient inputs")

	// ErrAddressNotFound signals that Normalize could not resolve a
	// free-text address to a real location — the geocoder returned no
	// feature, or the only candidate was outside the input zip's
	// département. Consumers (e.g. an HTTP layer mapping to 404) classify
	// with errors.Is(err, gazetteer.ErrAddressNotFound) instead of
	// importing the helpers/banx sentinels directly. The BANNormalizer
	// wraps banx.ErrNotFound and banx.ErrDepartmentMismatch with %w, so
	// errors.Is still matches those older sentinels too.
	ErrAddressNotFound = errors.New("gazetteer: address not found")

	// ErrUnsupportedPropertyType signals that the Source cannot reason
	// about Listing.PropertyType (e.g. DVF on parking lots). Stable
	// given the same property_type — callers may record a sentinel.
	ErrUnsupportedPropertyType = errors.New("gazetteer: unsupported property type")

	// ErrAntiBot signals an anti-bot interstitial (DataDome, captcha,
	// 403 with WAF banner). Treated as transient by the framework; the
	// caller's circuit breaker decides whether to back off.
	ErrAntiBot = errors.New("gazetteer: anti-bot challenge")

	// ErrUpstreamUnavailable signals that the upstream returned a
	// transient error (5xx, timeout). Retry-friendly.
	ErrUpstreamUnavailable = errors.New("gazetteer: upstream unavailable")

	// ErrUpstreamSchemaChanged signals that the Source could not parse
	// the upstream response because the layout no longer matches what
	// the Source expects. Actionable: the Source code needs updating.
	// Surfaced separately so operators can grep dashboards for it.
	ErrUpstreamSchemaChanged = errors.New("gazetteer: upstream schema changed (source parser outdated)")

	// ErrUpstreamPermanent signals that the upstream is broken in a way
	// the Source cannot work around (deprecated endpoint, withdrawn data
	// set). Caller should not retry until the Source code is updated.
	ErrUpstreamPermanent = errors.New("gazetteer: upstream permanent failure")

	// ErrSourceCircuitTripped is the canonical sentinel signalling that a
	// Source has aborted further work because its embedded circuit
	// breaker is open for the rest of this process — typically after a
	// streak of consecutive transport / 429 / antibot failures crossed
	// the per-Source threshold.
	//
	// Every shipped Source that operates a circuit breaker declares its
	// own per-package ErrCircuitTripped via NewCircuitTrippedError, so
	// callers may match either form:
	//
	//	errors.Is(err, dvf.ErrCircuitTripped)             // dvf-specific
	//	errors.Is(err, gazetteer.ErrSourceCircuitTripped) // cross-source
	//
	// The flag is process-scoped; a fresh run starts fresh.
	ErrSourceCircuitTripped = errors.New("gazetteer: source circuit tripped, skipping for the rest of this run")
)

// CircuitTrippedError is the canonical typed error a Source returns when
// its embedded circuit breaker is open for the rest of the process. The
// Error() string preserves the per-source phrasing operators see in
// logs; Is matches both the per-source sentinel (identity) and
// ErrSourceCircuitTripped (cross-source).
//
// Sources typically build a singleton with NewCircuitTrippedError in
// their package init and return that pointer from Query — errors.Is on
// the per-source sentinel then matches by pointer identity.
type CircuitTrippedError struct {
	// Source is the gazetteer.Source.Name() of the originating source
	// (e.g. "dvf"). Used to render the Error() message.
	Source string
}

// NewCircuitTrippedError returns a singleton-ready circuit-tripped
// error tagged with the source name. The returned pointer should be
// stored in a package-level `ErrCircuitTripped` var and reused; that
// way `errors.Is(err, MySource.ErrCircuitTripped)` matches by pointer
// identity while `errors.Is(err, gazetteer.ErrSourceCircuitTripped)`
// matches via the Is method.
func NewCircuitTrippedError(source string) *CircuitTrippedError {
	return &CircuitTrippedError{Source: source}
}

// Error implements error. The format mirrors the original per-source
// messages preserved across the migration to the canonical sentinel —
// log-pinning callers and dashboard regexes stay valid.
func (e *CircuitTrippedError) Error() string {
	return e.Source + ": upstream circuit tripped, skipping for the rest of this run"
}

// Is reports whether target is ErrSourceCircuitTripped, enabling the
// cross-source `errors.Is(err, gazetteer.ErrSourceCircuitTripped)` check.
// Per-source identity match falls through to the default pointer
// comparison the errors package does before calling Is.
func (e *CircuitTrippedError) Is(target error) bool {
	return target == ErrSourceCircuitTripped
}

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
)

package gazetteer

import (
	"context"
	"fmt"
)

// Status classifies the outcome of a Source.Query call. The Client sets
// it on the Result based on what the Source returned.
type Status int

const (
	// StatusOK indicates the Source returned a populated typed Data.
	StatusOK Status = iota

	// StatusOKEmpty indicates the Source ran successfully but the typed
	// Data reports IsEmpty() == true (no comparables, no DPE, etc.).
	// Callers can distinguish "no data" from "data" without inspecting
	// the typed payload.
	StatusOKEmpty

	// StatusSkippedPrereq indicates the Source skipped because Listing
	// inputs were missing or out-of-scope (ErrInsufficientInputs or
	// ErrUnsupportedPropertyType).
	StatusSkippedPrereq

	// StatusFailedTransient indicates a retry-friendly failure: network,
	// 5xx, generic error.
	StatusFailedTransient

	// StatusFailedAntiBot indicates an anti-bot interstitial.
	StatusFailedAntiBot

	// StatusFailedOutdated indicates the Source could not parse the
	// upstream response — the parser is outdated. Operator-actionable.
	StatusFailedOutdated

	// StatusFailedPermanent indicates a permanent upstream breakage.
	// Caller should not retry until the Source is fixed.
	StatusFailedPermanent
)

// String returns a stable, snake-case identifier suitable for logs and
// metrics labels.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusOKEmpty:
		return "ok_empty"
	case StatusSkippedPrereq:
		return "skipped_prereq"
	case StatusFailedTransient:
		return "failed_transient"
	case StatusFailedAntiBot:
		return "failed_antibot"
	case StatusFailedOutdated:
		return "failed_outdated"
	case StatusFailedPermanent:
		return "failed_permanent"
	default:
		return fmt.Sprintf("unknown_%d", int(s))
	}
}

// Source is the central abstraction. A Source is a named, versioned data
// origin that produces a typed Data payload for a given Listing.
//
// Shared infrastructure (HTTP client, logger, debug-dump flag, cache) is
// propagated via ctx — see context.go — so Sources never need to receive
// these as constructor parameters. Implementations must respect ctx.Done().
type Source interface {
	// Name is the short identifier ("dvf", "carteloyers", …). Stable
	// across versions; used as the registry key and the Dossier.Results
	// map key. Per-package convention: also exposed as `const Name`.
	Name() string

	// Version is a monotonic integer bumped when the Source's internal
	// logic changes. Callers gate cache invalidation on it.
	Version() int

	// Query produces a typed Data payload. Sources return idiomatic Go
	// errors; the framework wraps the (Data, error) pair into a Result
	// envelope (see Result in result.go).
	Query(ctx context.Context, listing Listing) (any, error)
}

// EmptyReporter is an optional interface a Source's typed Data MAY
// implement to signal "I ran successfully but found no useful data".
// When Data satisfies EmptyReporter and IsEmpty() returns true, the
// framework records Status == StatusOKEmpty instead of StatusOK.
type EmptyReporter interface {
	IsEmpty() bool
}

// QueryWither is an opt-in interface a Source MAY implement to accept
// extra arguments beyond Listing on a side-entry Query path. The
// framework's Client.Collect always calls Source.Query — QueryWith is
// for direct callers that need to pass additional context (a
// pre-resolved id, a session token, a per-call timeout override).
//
// The args slice is a Source-specific contract; each implementing
// Source documents the shape it accepts. Sources that don't need
// extra arguments leave the interface unimplemented; callers fall
// back to Source.Query in that case.
//
// Generic call pattern from a consumer that doesn't know the concrete
// Source type:
//
//	if q, ok := src.(gazetteer.QueryWither); ok {
//	    data, err := q.QueryWith(ctx, listing, myExtraArg)
//	    ...
//	} else {
//	    data, err := src.Query(ctx, listing)
//	}
//
// Implementations should treat unrecognised args as a degenerate case
// and fall back to the Listing-only Query path rather than returning
// an error, so callers can pass best-effort hints without coupling.
type QueryWither interface {
	QueryWith(ctx context.Context, listing Listing, args ...any) (any, error)
}

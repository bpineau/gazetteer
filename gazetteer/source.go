package gazetteer

import (
	"context"
	"errors"
)

// Status classifies the outcome of a Source.Query call. The Client sets
// it on the Result based on what the Source returned. The underlying
// type is a string so the same Status round-trips through JSON, log
// records, metric labels, and Go maps without conversion or a dual
// marshal/parse code path.
type Status string

const (
	// StatusOK indicates the Source returned a populated typed Data.
	StatusOK Status = "ok"

	// StatusOKEmpty indicates the Source ran successfully but the typed
	// Data reports IsEmpty() == true (no comparables, no DPE, etc.).
	// Callers can distinguish "no data" from "data" without inspecting
	// the typed payload.
	StatusOKEmpty Status = "ok_empty"

	// StatusSkippedPrereq indicates the Source skipped because Listing
	// inputs were missing or out-of-scope (ErrInsufficientInputs or
	// ErrUnsupportedPropertyType).
	StatusSkippedPrereq Status = "skipped_prereq"

	// StatusFailedTransient indicates a retry-friendly failure: network,
	// 5xx, generic error.
	StatusFailedTransient Status = "failed_transient"

	// StatusFailedAntiBot indicates an anti-bot interstitial.
	StatusFailedAntiBot Status = "failed_antibot"

	// StatusFailedOutdated indicates the Source could not parse the
	// upstream response — the parser is outdated. Operator-actionable.
	StatusFailedOutdated Status = "failed_outdated"

	// StatusFailedPermanent indicates a permanent upstream breakage.
	// Caller should not retry until the Source is fixed.
	StatusFailedPermanent Status = "failed_permanent"
)

// String returns the underlying string for compatibility with consumers
// that expect a fmt.Stringer. Equivalent to a direct cast.
func (s Status) String() string {
	if s == "" {
		return "unknown_empty"
	}
	return string(s)
}

// Source is the central abstraction. A Source is a named, versioned data
// origin that produces a typed Data payload for a given Listing.
//
// Shared infrastructure (HTTP client, logger, cache) is
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

// QueryTyped runs src.Query and asserts the returned payload to R — the
// shared body of every source package's atomic Query helper:
//
//	func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
//		return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
//	}
//
// A payload of the wrong type (a Source whose Query violates its own
// contract) is an error, prefixed with the source's Name.
func QueryTyped[R any](ctx context.Context, src Source, l Listing) (R, error) {
	var zero R
	data, err := src.Query(ctx, l)
	if err != nil {
		return zero, err
	}
	r, ok := data.(R)
	if !ok {
		return zero, errors.New(src.Name() + ": typed result mismatch")
	}
	return r, nil
}

// EmptyReporter is an optional interface a Source's typed Data MAY
// implement to signal "I ran successfully but found no useful data".
// When Data satisfies EmptyReporter and IsEmpty() returns true, the
// framework records Status == StatusOKEmpty instead of StatusOK.
type EmptyReporter interface {
	IsEmpty() bool
}

// Evidencer is an opt-in interface a Source's typed Data MAY implement
// to expose its Evidence sidecar through the framework Result envelope.
// When Data satisfies Evidencer, the framework stamps Result.Evidence
// with what Evidence() returns; consumers can then read
// dossier.Results["dvf"].Evidence without type-asserting on the typed
// Data.
//
// Implementations typically return the same value that lives on the
// typed Result struct as a `json:"-"` Evidence field — i.e. a single
// shared instance, no defensive copy.
type Evidencer interface {
	Evidence() any
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

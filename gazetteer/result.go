package gazetteer

import (
	"encoding/json"
	"time"
)

// Result is the framework envelope around one Source's contribution to a
// Dossier. Built by the Client — never directly by Sources.
//
// # Conventions on Data
//
// Every shipped Source returns the same shape for Data: a pointer to a
// package-defined `Result` struct (e.g. `*dvf.Result`, `*osm.Result`).
// The Source registers a factory for that struct via gazetteer.Register
// in init() so Dossier JSON unmarshal can reconstitute concrete types.
//
// The typed Data MAY:
//   - Implement EmptyReporter to signal "successful but no useful data";
//     the framework then records Status == StatusOKEmpty automatically.
//   - Carry a separate `Evidence` field (tagged `json:"-"`) holding
//     reproducibility metadata (input fingerprint, ladder tier used,
//     resolver provenance). This is a strong convention across every
//     shipped Source but not part of the framework contract — callers
//     read it directly from the typed Data once retrieved via Get[T].
//
// The typed Data MUST be safe to JSON-marshal and unmarshal via the
// factory registered in gazetteer.Register; otherwise Dossier
// roundtrip will silently drop the payload.
type Result struct {
	Name      string // == Source.Name()
	Version   int    // == Source.Version() at the time of Query
	Status    Status
	FetchedAt time.Time

	// ElapsedMS is the wall-clock time the Source's Query took, in
	// whole milliseconds — populated by the Client for every run
	// (success, empty, failure, timeout, or recovered panic). It lets a
	// caller see who paid what latency in a Collect, and is the signal
	// behind Builder.WithPerSourceTimeout: a Source whose ElapsedMS
	// approaches the configured deadline is the one dragging the Dossier.
	// Zero for a Result built by hand (e.g. in tests) rather than by
	// Collect.
	ElapsedMS int64

	// Err is non-nil iff Status is a failure status. JSON round-trip
	// caveat: Err is serialised as its message string, so after a
	// Dossier round-trip errors.Is/As matching stops working — gate
	// retry logic on Status (which survives), not on the error chain.
	Err error

	Data any // typed payload struct; may be non-nil even for StatusOKEmpty

	// Evidence is the reproducibility sidecar — input fingerprint,
	// ladder tier used, resolver provenance, sample-size hints. The
	// framework populates it during runOne when Data implements
	// Evidencer; consumers reach it through the envelope without
	// type-asserting on Data:
	//
	//	if ev, ok := r.Evidence.(*dvf.Evidence); ok { ... }
	//
	// Sources that don't implement Evidencer leave this nil. The
	// typed Data MAY still carry its own Evidence field — the
	// framework slot is a uniform-access convenience, not a
	// replacement for the per-Source typed shape.
	//
	// JSON round-trip caveat: Evidence is marshaled into the wire form
	// (under "evidence"), but there is no per-source factory to restore
	// the concrete type, so after unmarshal Evidence holds the raw
	// json.RawMessage. Audit consumers can persist or display it;
	// typed access only exists in the process that ran the Query.
	Evidence any
}

// IsEmpty reports whether the underlying typed Data implements
// EmptyReporter and reports itself as empty. Returns false when Data is
// nil, when Data does not implement EmptyReporter, or when IsEmpty()
// returns false on the typed payload.
//
// This is a convenience over a type assertion — callers consuming
// JSON-roundtripped Dossiers can ask `r.IsEmpty()` without knowing the
// concrete Data type.
func (r Result) IsEmpty() bool {
	if r.Data == nil {
		return false
	}
	er, ok := r.Data.(EmptyReporter)
	if !ok {
		return false
	}
	return er.IsEmpty()
}

// MarshalJSON emits a stable wire representation. Err is serialised as a
// plain string (Go's error type does not implement json.Marshaler), Data
// is delegated to the typed payload's own marshaler, and Evidence is
// marshaled from the typed sidecar (restored as raw JSON on unmarshal —
// see the Evidence field doc).
func (r Result) MarshalJSON() ([]byte, error) {
	type wire struct {
		Name      string          `json:"name"`
		Version   int             `json:"version"`
		Status    Status          `json:"status"`
		FetchedAt time.Time       `json:"fetched_at,omitzero"`
		ElapsedMS int64           `json:"elapsed_ms,omitempty"`
		Err       string          `json:"err,omitempty"`
		Data      json.RawMessage `json:"data,omitempty"`
		Evidence  json.RawMessage `json:"evidence,omitempty"`
	}
	w := wire{
		Name:      r.Name,
		Version:   r.Version,
		Status:    r.Status,
		FetchedAt: r.FetchedAt,
		ElapsedMS: r.ElapsedMS,
	}
	if r.Err != nil {
		w.Err = r.Err.Error()
	}
	if r.Data != nil {
		raw, err := json.Marshal(r.Data)
		if err != nil {
			return nil, err
		}
		w.Data = raw
	}
	if r.Evidence != nil {
		raw, err := json.Marshal(r.Evidence)
		if err != nil {
			return nil, err
		}
		w.Evidence = raw
	}
	return json.Marshal(w)
}

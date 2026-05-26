package gazetteer

import (
	"encoding/json"
	"time"
)

// Result is the framework envelope around one Source's contribution to a
// Dossier. Built by the Client — never directly by Sources.
type Result struct {
	Name      string // == Source.Name()
	Version   int    // == Source.Version() at the time of Query
	Status    Status
	InputHash string
	FetchedAt time.Time
	Err       error // non-nil iff Status is a failure status
	Data      any   // typed payload struct; may be non-nil even for StatusOKEmpty
}

// MarshalJSON emits a stable wire representation. Err is serialised as a
// plain string (Go's error type does not implement json.Marshaler), and
// Data is delegated to the typed payload's own marshaler.
func (r Result) MarshalJSON() ([]byte, error) {
	type wire struct {
		Name      string          `json:"name"`
		Version   int             `json:"version"`
		Status    string          `json:"status"`
		InputHash string          `json:"input_hash,omitempty"`
		FetchedAt time.Time       `json:"fetched_at,omitzero"`
		Err       string          `json:"err,omitempty"`
		Data      json.RawMessage `json:"data,omitempty"`
	}
	w := wire{
		Name:      r.Name,
		Version:   r.Version,
		Status:    r.Status.String(),
		InputHash: r.InputHash,
		FetchedAt: r.FetchedAt,
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
	return json.Marshal(w)
}

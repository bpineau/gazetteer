package gazetteer

import (
	"encoding/json"
	"fmt"
	"time"
)

// Dossier is the aggregated output of a Client.Collect call. Keyed by
// Source.Name(), each Result either carries typed Data (StatusOK /
// StatusOKEmpty) or a failure indicator.
type Dossier struct {
	Listing    Listing           `json:"listing"`
	Results    map[string]Result `json:"results"`
	StartedAt  time.Time         `json:"started_at,omitzero"`
	FinishedAt time.Time         `json:"finished_at,omitzero"`
}

// OK reports whether the named Source returned successfully WITH useful
// data (StatusOK). StatusOKEmpty returns false here — use Get[T] to
// inspect the typed payload's IsEmpty() if you care.
func (d Dossier) OK(name string) bool {
	r, ok := d.Results[name]
	return ok && r.Status == StatusOK
}

// Failed returns the subset of Results whose Status is not OK or
// OKEmpty. Useful for partial-degradation surfacing.
func (d Dossier) Failed() map[string]Result {
	out := map[string]Result{}
	for k, r := range d.Results {
		if r.Status != StatusOK && r.Status != StatusOKEmpty {
			out[k] = r
		}
	}
	return out
}

// Get extracts a typed Data value from a Dossier. Returns (zero, false)
// when the Source is absent, failed, or the Data does not match T.
// Both StatusOK and StatusOKEmpty yield (data, true) — the caller can
// distinguish "has useful data" via the source-specific IsEmpty() if
// the Data type implements EmptyReporter.
func Get[T any](d Dossier, name string) (T, bool) {
	var zero T
	r, ok := d.Results[name]
	if !ok {
		return zero, false
	}
	// A zero-value Status ("") is treated as OK so consumers that
	// construct Result literals in tests without explicitly stamping
	// Status still see their Data — matches the historical iota=0=OK
	// behaviour of the int Status type.
	switch r.Status {
	case "", StatusOK, StatusOKEmpty:
	default:
		return zero, false
	}
	typed, ok := r.Data.(T)
	return typed, ok
}

// UnmarshalJSON reconstitutes a Dossier from wire bytes. Each Result's
// Data is materialised via the registered factory for the Source name;
// unknown names yield a Result with Data == nil but otherwise correct
// envelope fields, so failures degrade gracefully.
func (d *Dossier) UnmarshalJSON(b []byte) error {
	type wireResult struct {
		Name      string          `json:"name"`
		Version   int             `json:"version"`
		Status    string          `json:"status"`
		InputHash string          `json:"input_hash"`
		FetchedAt time.Time       `json:"fetched_at"`
		Err       string          `json:"err"`
		Data      json.RawMessage `json:"data"`
	}
	type wireDossier struct {
		Listing    Listing               `json:"listing"`
		Results    map[string]wireResult `json:"results"`
		StartedAt  time.Time             `json:"started_at"`
		FinishedAt time.Time             `json:"finished_at"`
	}
	var w wireDossier
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	d.Listing = w.Listing
	d.StartedAt = w.StartedAt
	d.FinishedAt = w.FinishedAt
	d.Results = make(map[string]Result, len(w.Results))
	for k, r := range w.Results {
		out := Result{
			Name:      r.Name,
			Version:   r.Version,
			Status:    Status(r.Status),
			InputHash: r.InputHash,
			FetchedAt: r.FetchedAt,
		}
		if r.Err != "" {
			out.Err = fmt.Errorf("%s", r.Err)
		}
		if len(r.Data) > 0 {
			if factory := Lookup(r.Name); factory != nil {
				val := factory()
				if err := json.Unmarshal(r.Data, val); err == nil {
					out.Data = val
				}
			}
			// Unknown name: leave Data nil (degraded mode).
		}
		d.Results[k] = out
	}
	return nil
}

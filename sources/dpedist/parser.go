package dpedist

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/helpers/stats"
)

// apiResponse mirrors the data-fair `values_agg` envelope this Source
// consumes. Only the fields used downstream are unmarshalled.
type apiResponse struct {
	Total      int `json:"total"`
	TotalOther int `json:"total_other"`
	Aggs       []struct {
		Value any `json:"value"` // string class OR null
		Total int `json:"total"`
	} `json:"aggs"`
}

// Parse turns the upstream JSON body into a Result minus the
// commune-side stamping (Confidence + Evidence are set by the caller).
// Returns a non-nil *Result on success (possibly with NbTotal == 0);
// returns an error when the body could not be decoded.
func Parse(body []byte) (*Result, error) {
	if len(body) == 0 {
		return nil, errors.New("dpedist: empty body")
	}
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("dpedist: decode json: %w", err)
	}

	out := &Result{
		Counts: map[Label]int{},
	}
	for _, row := range resp.Aggs {
		l := foldLabel(row.Value)
		out.Counts[l] += row.Total
	}
	// Fold total_other rows the API may keep outside the bucket array
	// onto LabelN so the integral is preserved.
	if resp.TotalOther > 0 {
		out.Counts[LabelN] += resp.TotalOther
	}

	// Recompute total from buckets (more reliable than resp.Total when
	// the upstream paginated past the size we asked for).
	for _, n := range out.Counts {
		out.NbTotal += n
	}
	// Drop the zero-count buckets we may have created above so the
	// rendered map stays compact.
	for k, n := range out.Counts {
		if n == 0 {
			delete(out.Counts, k)
		}
	}
	if out.NbTotal == 0 {
		// Drop the zero-bucket map so IsEmpty() / serialisation stay
		// idiomatic.
		out.Counts = nil
		return out, nil
	}

	out.Shares = make(map[Label]float64, len(out.Counts))
	for l, n := range out.Counts {
		out.Shares[l] = stats.Round(100*float64(n)/float64(out.NbTotal), 1)
	}
	out.PassoireSharePct = stats.Round(out.Shares[LabelF]+out.Shares[LabelG], 1)
	out.EfficientSharePct = stats.Round(out.Shares[LabelA]+out.Shares[LabelB], 1)
	return out, nil
}

// foldLabel normalises the upstream `value` field onto the package's
// Label enum. The upstream emits the uppercase letter A..G for legal
// classes, may emit "N" or "Non évalué" or null for un-evaluated rows.
// Unknown / null values map to LabelN.
func foldLabel(v any) Label {
	if v == nil {
		return LabelN
	}
	s, ok := v.(string)
	if !ok {
		return LabelN
	}
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "A":
		return LabelA
	case "B":
		return LabelB
	case "C":
		return LabelC
	case "D":
		return LabelD
	case "E":
		return LabelE
	case "F":
		return LabelF
	case "G":
		return LabelG
	default:
		return LabelN
	}
}

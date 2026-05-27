package filosofi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "filosofi"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial port from internal/core/enrich/rental/filosofi.
//     Per-commune Filosofi 2021 indicators (median revenu disponible +
//     minima sociaux %). Risk classifier thresholds calibrated against
//     the 2021 national distribution.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source (e.g.
// a rental wrapper) can mirror it without reaching into
// the package internals.
const Version = sourceVersion

// Options configures a filosofi Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the INSEE Filosofi (revenu
// disponible des ménages) commune dataset using an embedded JSON
// table. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a filosofi Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs — the wrapper is responsible
//     for resolving INSEE from (zip, city).
//  2. Look up the commune in the embedded Filosofi index.
//  3. Compute the risk flag (low / medium / high / unknown).
//  4. Return (*Result, nil). Missing communes surface as IsEmpty().
//
// Property type is irrelevant for this source — the Filosofi profile
// applies to the whole commune.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("filosofi: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("filosofi: load dataset: %w", err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:             insee,
		DataYear:          idx.Meta.DataYear,
		NationalMedianEUR: idx.Meta.NationalMedianEUR,
	}

	e, ok := idx.Lookup(insee)
	if !ok || e.MedianEUR <= 0 {
		return &Result{
			Flag:       RiskUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	flag := classifyRisk(e)
	conf := ConfidenceMedium
	if e.MinimaPct > 0 {
		conf = ConfidenceHigh
	}
	return &Result{
		MedianEUR:  e.MedianEUR,
		MinimaPct:  e.MinimaPct,
		Flag:       flag,
		Confidence: conf,
		Evidence:   ev,
	}, nil
}

// classifyRisk applies the income-risk thresholds calibrated against
// the 2021 national distribution :
//
//	low    : median ≥ 25 000 €   AND minima ≤ 2.5 %
//	high   : median ≤ 18 000 €   OR  minima ≥ 5.0 %
//	medium : everything else with a populated median
//	unknown: commune missing from the Filosofi dataset (handled by
//	         caller)
func classifyRisk(e Entry) RiskFlag {
	switch {
	case e.MedianEUR >= 25000 && (e.MinimaPct == 0 || e.MinimaPct <= 2.5):
		return RiskLow
	case e.MedianEUR <= 18000 || (e.MinimaPct > 0 && e.MinimaPct >= 5.0):
		return RiskHigh
	default:
		return RiskMedium
	}
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("filosofi: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

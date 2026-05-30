package qpv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "qpv"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the QPV 2024 list (decree
//     2023-1314) at commune granularity — answers "does this commune
//     host one or more QPVs?".
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a qpv Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the per-commune QPV lookup
// using an embedded JSON. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a qpv Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// extract to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//  2. Look up the commune in the embedded QPV index.
//  3. Return (*Result, nil). Communes without QPV surface as
//     IsEmpty() (the vast majority — only ~840 communes host QPVs).
//
// Property type is irrelevant — QPV designation is geographic. Note
// also that this Source operates at the commune level, NOT the
// address level: a positive result tells the caller the commune
// contains QPVs, not that the specific listing is inside one.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("qpv: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("qpv: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	// Paris / Lyon / Marseille arrondissements share the parent
	// commune's QPV list — the ANCT publishes QPVs against the
	// parent commune INSEE only (75056 / 69123 / 13055).
	insee = communes.FoldArrondissement(insee)

	ev := Evidence{
		INSEE:            insee,
		RowCountCommunes: idx.Count(),
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	ev.CommuneLabel = e.Label
	return &Result{
		HasQPV:     true,
		QPVCount:   len(e.QPVs),
		QPVs:       copyQPVs(e.QPVs),
		Confidence: ConfidenceHigh,
		Evidence:   ev,
	}, nil
}

// copyQPVs returns a shallow copy of s. The Source's embedded
// singleton index is shared across all Query calls; without the copy
// a caller mutating Result.QPVs would corrupt the next call's
// reading. Cheap (most QPV-hosting communes carry 1-3 entries; the
// largest tops out around 30).
func copyQPVs(s []QPV) []QPV {
	if s == nil {
		return nil
	}
	out := make([]QPV, len(s))
	copy(out, s)
	return out
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
		return nil, errors.New("qpv: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

package anct

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "anct"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Merges three data.gouv.fr ANCT datasets
//     (Action Cœur de Ville, Petites Villes de Demain, Opérations de
//     Revitalisation de Territoire) into a single per-commune lookup.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures an anct Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the merged ANCT territorial
// programmes using an embedded JSON. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an anct Source. Zero-valued Options is fine.
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
//  2. Look up the commune in the merged ACV / PVD / ORT index.
//  3. Return (*Result, nil). Communes not enrolled in any of the
//     three programmes surface as IsEmpty() (the vast majority — only
//     ~2 400 communes participate).
//
// Property type is irrelevant — programme participation is a
// commune-wide attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("anct: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("anct: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

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
		ACV:                 e.ACV,
		PVD:                 e.PVD,
		ORT:                 e.ORT,
		Programmes:          programmeList(e.ACV, e.PVD, e.ORT),
		DenormandieEligible: e.ORT,
		ACVSignedAt:         e.ACVSignedAt,
		PVDSignedAt:         e.PVDSignedAt,
		ORTSignedAt:         e.ORTSignedAt,
		Confidence:          ConfidenceHigh,
		Evidence:            ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

// QueryResult is Query with the package's typed Result — for callers
// holding a constructed Source instance. Equivalent to the package-level
// Query helper without rebuilding the Source per call.
func (s *Source) QueryResult(ctx context.Context, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, s, l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

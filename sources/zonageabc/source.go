package zonageabc

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "zonageabc"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial. Per-commune A/Abis/B1/B2/C lookup against the
//     5 septembre 2025 arrêté revision (data.gouv.fr).
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a zonageabc Source. The zero value is usable.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the official A bis / A / B1 /
// B2 / C zonage published by the Ministère du Logement. Use NewSource
// to construct.
type Source struct {
	opts Options
}

// NewSource builds a zonageabc Source. Zero-valued Options is fine.
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
//  2. Look up the commune in the embedded zonage index.
//  3. Return (*Result, nil). Missing communes surface as IsEmpty().
//
// Property type is irrelevant: the zonage classifies the whole
// commune.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("zonageabc: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("zonageabc: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	// Fold Paris / Lyon / Marseille arrondissement INSEE codes onto
	// their parent commune — the official dataset only carries parent
	// commune rows (75056 / 69123 / 13055).
	folded := communes.FoldArrondissement(insee)

	ev := Evidence{
		INSEE:         insee,
		EffectiveDate: idx.Meta.EffectiveDate,
	}
	if folded != insee {
		ev.LookupINSEE = folded
	}

	z, ok := idx.Lookup(folded)
	if !ok {
		return &Result{
			Zone:         ZoneUnknown,
			TensionScore: -1,
			Confidence:   ConfidenceNone,
			Evidence:     ev,
		}, nil
	}

	return &Result{
		Zone:         z,
		TensionScore: TensionScore(z),
		Confidence:   ConfidenceHigh,
		Evidence:     ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

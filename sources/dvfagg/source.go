package dvfagg

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Options configures a dvfagg Source. Zero value is usable (embedded only).
type Options struct {
	// DataDir lets refreshed copies in the datadir override the embedded CSV.
	DataDir string
	// Index overrides Load when non-nil; tests inject a stub to avoid the
	// embedded singleton and the sync.Once cache.
	Index *Index
}

// Source implements gazetteer.Source + gazetteer.DatasetProvider for the
// offline per-commune DVF aggregate.
type Source struct{ opts Options }

// NewSource builds a dvfagg Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return Version }

// Datasets implements gazetteer.DatasetProvider (refresh tooling).
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{theSet} }

// Query implements gazetteer.Source: returns the commune aggregate for the
// listing's INSEE. Without an INSEE it emits gazetteer.ErrInsufficientInputs;
// a present-but-unmatched commune surfaces as IsEmpty() (no qualifying sale).
func (s *Source) Query(_ context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("dvfagg: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}
	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("dvfagg: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}
	r, _ := idx.Lookup(insee)
	r.Evidence = Evidence{INSEE: insee}
	return &r, nil
}

// Query is the atomic helper for callers who don't want the builder. The error
// is non-nil only when the Source failed; a successful but empty response still
// returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

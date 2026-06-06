package dvfagg

import (
	"context"
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
// listing's INSEE. Empty Result (IsEmpty) when the commune has no sales.
func (s *Source) Query(_ context.Context, l gazetteer.Listing) (any, error) {
	idx := s.opts.Index
	if idx == nil {
		var err error
		idx, err = Load(s.opts.DataDir)
		if err != nil {
			return nil, err
		}
	}
	insee := strings.TrimSpace(l.INSEE)
	r, _ := idx.Lookup(insee)
	return &r, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

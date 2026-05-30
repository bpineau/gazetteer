package iris

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; the Dossier results key.
const Name = "iris"

// sourceVersion bumps when the Source's internal logic changes.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it.
const Version = sourceVersion

// Options configures an iris Source. The zero value is usable.
type Options struct {
	// Index overrides the lazily-loaded singleton (tests inject a stub).
	Index *Index

	// DataDir is the gazetteer data directory; a refreshed copy there overrides
	// the embedded contours. Empty means embedded-only.
	DataDir string
}

// Source implements gazetteer.Source for the IRIS locator and
// gazetteer.IRISResolver for the Normalizer. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an iris Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

func (s *Source) index() (*Index, error) {
	if s.opts.Index != nil {
		return s.opts.Index, nil
	}
	return Load(s.opts.DataDir)
}

// ResolveIRIS implements gazetteer.IRISResolver: it returns the IRIS code
// containing (lat, lon), so a BANNormalizer can populate Listing.IRIS.
//
// A load failure or an out-of-perimeter point yields ok=false: resolution is
// best-effort and must never sink normalization. The error is intentionally
// swallowed and NOT observable on this path — a corrupt datadir artifact would
// silently resolve every address to empty IRIS; the observable signal is the
// Source.Query path, which surfaces ErrUpstreamPermanent.
func (s *Source) ResolveIRIS(lat, lon float64) (string, bool) {
	idx, err := s.index()
	if err != nil {
		return "", false
	}
	code, _, _, ok := idx.resolve(lat, lon)
	return code, ok
}

// Query implements gazetteer.Source. It returns the IRIS containing the
// listing's coordinates. When the Listing already carries a resolved IRIS (set
// by the Normalizer), it reuses it and only looks up the name/type, avoiding a
// second point-in-polygon pass.
//
// Missing Lat/Lon (and no pre-resolved IRIS) → gazetteer.ErrInsufficientInputs.
// A point outside the perimeter → a non-nil *Result with IsEmpty() == true.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	idx, err := s.index()
	if err != nil {
		return nil, fmt.Errorf("iris: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
	}

	// Fast path: the Normalizer already resolved the IRIS.
	if code := strings.TrimSpace(l.IRIS); code != "" {
		nom, typ, ok := idx.lookupCode(code)
		if ok {
			return &Result{CodeIRIS: code, NomIRIS: nom, TypIRIS: typ, Confidence: ConfidenceHigh,
				Evidence: Evidence{Source: "listing", PerimeterIRIS: idx.Count()}}, nil
		}
		// Code carried but not in this (IDF) perimeter — it was resolved
		// elsewhere. With coordinates we re-resolve below; without them, surface
		// the carried code (the locator) rather than dropping it on an
		// insufficient-inputs error. Name/type stay empty (unknown here).
		if l.Lat == nil || l.Lon == nil {
			return &Result{CodeIRIS: code, Confidence: ConfidenceHigh,
				Evidence: Evidence{Source: "listing", PerimeterIRIS: idx.Count()}}, nil
		}
	}

	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("iris: %w: missing lat/lon", gazetteer.ErrInsufficientInputs)
	}
	lat, lon := *l.Lat, *l.Lon
	// (0,0) is the "unset coords" sentinel — a real listing never sits on Null
	// Island, which is far from any covered IRIS anyway.
	if lat == 0 && lon == 0 {
		return nil, fmt.Errorf("iris: %w: lat/lon=0,0 sentinel", gazetteer.ErrInsufficientInputs)
	}

	ev := Evidence{ListingLat: lat, ListingLon: lon, Source: "geometry", PerimeterIRIS: idx.Count()}
	code, nom, typ, ok := idx.resolve(lat, lon)
	if !ok {
		return &Result{Evidence: ev}, nil // outside the perimeter
	}
	return &Result{CodeIRIS: code, NomIRIS: nom, TypIRIS: typ, Confidence: ConfidenceHigh, Evidence: ev}, nil
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("iris: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

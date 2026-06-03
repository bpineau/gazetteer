package sitadel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "sitadel"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the SDES Sitadel 2026-06 millésime
//     (per-commune dwellings authorised + started, 2013→2025).
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it
// without reaching into the package internals.
const Version = sourceVersion

// avgWindow is the number of trailing years averaged for the 5-year means.
const avgWindow = 5

// Options configures a sitadel Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub here;
	// production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy of
	// the processed artifact found there takes precedence over the embedded
	// one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for per-commune housing-construction
// dynamics using an embedded gzipped JSON. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a sitadel Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

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
//  2. Fold Paris/Lyon/Marseille arrondissements onto the parent commune
//     (the upstream keys construction against the parent aggregate).
//  3. Look up the commune; project the compact per-year arrays into a Result.
//  4. Communes absent (or with no non-zero authorised data) surface as
//     IsEmpty() == true.
//
// Property type and surface are irrelevant — construction dynamics are a
// commune-wide attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("sitadel: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("sitadel: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	insee = communes.FoldArrondissement(insee)

	ev := Evidence{
		INSEE:            insee,
		DataMillesime:    idx.Meta.DataMillesime,
		RowCountCommunes: idx.Count(),
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{Confidence: ConfidenceNone, Evidence: ev}, nil
	}
	res := project(e)
	res.Evidence = ev
	res.Evidence.RowYears = countPresentYears(e.Auth)
	return res, nil
}

// project turns a compact Entry into a Result.
func project(e Entry) *Result {
	r := &Result{Confidence: ConfidenceNone}

	// Series: full Auth array, missing years rendered as 0 for the sparkline.
	if len(e.Auth) > 0 {
		r.SeriesStartYear = e.YearStart
		r.AuthorizedSeries = make([]int, len(e.Auth))
		for i, v := range e.Auth {
			if v == missing {
				r.AuthorizedSeries[i] = 0
			} else {
				r.AuthorizedSeries[i] = v
			}
		}
	}

	// Latest authorised: the newest year with a present (non-missing) value.
	if i, v, ok := latestPresent(e.Auth); ok {
		r.AuthorizedLatest = v
		r.LatestYear = e.YearStart + i
	}

	// Confidence is "high" only when the commune carries a non-zero
	// authorised value somewhere in the series — a populated supply signal.
	// An all-zero/all-missing series stays IsEmpty (the transform drops these,
	// but project must agree).
	if anyPositive(e.Auth) {
		r.Confidence = ConfidenceHigh
	}

	// Latest started: the newest year with a present started value.
	if i, v, ok := latestPresent(e.Started); ok {
		r.StartedLatest = v
		r.StartedLatestYear = e.YearStart + i
	}

	r.AuthorizedAvg5y = trailingMean(e.Auth)
	r.StartedAvg5y = trailingMean(e.Started)
	r.CollectifSharePct = collectifShare(e)

	return r
}

// latestPresent returns the index, value and ok=true of the newest
// non-missing element, scanning from the end.
func latestPresent(xs []int) (int, int, bool) {
	for i := len(xs) - 1; i >= 0; i-- {
		if xs[i] != missing {
			return i, xs[i], true
		}
	}
	return 0, 0, false
}

// trailingMean averages up to the last avgWindow present (non-missing) values,
// rounded to one decimal. Returns 0 when none present.
func trailingMean(xs []int) float64 {
	sum, n := 0, 0
	for i := len(xs) - 1; i >= 0 && n < avgWindow; i-- {
		if xs[i] == missing {
			continue
		}
		sum += xs[i]
		n++
	}
	if n == 0 {
		return 0
	}
	return round1(float64(sum) / float64(n))
}

// collectifShare is Collectif-authorised over Tous-authorised, summed over the
// last avgWindow years that carry an authorised value, as a percent (one
// decimal). Returns 0 when no authorised dwellings in the window.
func collectifShare(e Entry) float64 {
	var totAuth, totColl int
	n := 0
	for i := len(e.Auth) - 1; i >= 0 && n < avgWindow; i-- {
		if e.Auth[i] == missing {
			continue
		}
		totAuth += e.Auth[i]
		if i < len(e.CollAuth) && e.CollAuth[i] != missing {
			totColl += e.CollAuth[i]
		}
		n++
	}
	if totAuth <= 0 {
		return 0
	}
	return round1(float64(totColl) / float64(totAuth) * 100)
}

// anyPositive reports whether xs has a non-missing, strictly-positive value.
func anyPositive(xs []int) bool {
	for _, v := range xs {
		if v != missing && v > 0 {
			return true
		}
	}
	return false
}

// countPresentYears counts non-missing elements of xs.
func countPresentYears(xs []int) int {
	n := 0
	for _, v := range xs {
		if v != missing {
			n++
		}
	}
	return n
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// Query is the atomic helper for callers who don't want the builder. The error
// is non-nil only when the Source failed; a successful but empty response
// still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("sitadel: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

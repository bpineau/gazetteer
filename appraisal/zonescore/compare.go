package zonescore

import (
	"context"
	"sort"
	"sync"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// Collector is the subset of *gazetteer.Client that Compare needs: aggregate one
// listing's sources into a Dossier. Taking the interface (not the concrete
// Client) keeps Compare testable with a stub.
type Collector interface {
	Collect(ctx context.Context, l gazetteer.Listing) gazetteer.Dossier
}

// Comparison ranks several candidate zones for the same yield-first thesis.
type Comparison struct {
	// Entries are the candidates ranked best-first by composite zone score.
	Entries []ComparisonEntry `json:"entries"`
}

// ComparisonEntry is one candidate's collected data, score and headline metrics.
type ComparisonEntry struct {
	// Listing is the (already normalized) input candidate.
	Listing gazetteer.Listing `json:"listing"`

	// Dossier is the full aggregated data for the candidate.
	Dossier gazetteer.Dossier `json:"dossier"`

	// Score is the yield-first composite zone score and its axis breakdown.
	Score Score `json:"score"`

	// YieldPct is the gross rental yield (rent×12/price), 0 when price or rent
	// is unavailable.
	YieldPct float64 `json:"yield_pct,omitempty"`

	// PriceEURPerM2 / RentEURPerM2 are the consolidated €/m² readings driving
	// the yield (rent is €/m²/month).
	PriceEURPerM2 float64 `json:"price_eur_per_m2,omitempty"`
	RentEURPerM2  float64 `json:"rent_eur_per_m2,omitempty"`

	// Rank is the 1-based position in the comparison (1 = best).
	Rank int `json:"rank"`
}

// maxParallelListings bounds how many candidate Dossiers are collected at once.
// Each Collect already fans its own sources out concurrently, so this caps the
// listing dimension of the N×sources request fan-out (a guard for callers that
// pass a large slice — the ctx deadline is a single shared budget across all
// candidates).
const maxParallelListings = 8

// Compare collects each candidate listing's Dossier in parallel, scores it with
// the same (yield-first) profile, and returns them ranked best-first. A
// candidate whose yield is KNOWN (the rendement axis present) always outranks
// one whose yield is unknown; ties then break on composite, yield and address,
// so the order is deterministic.
//
// listings should already be normalized (coordinates / INSEE populated) — the
// scorers need the same inputs Collect does. opts tunes the scoring profile (see
// Options); the same profile applies to every candidate for a fair comparison.
// At most maxParallelListings Dossiers are collected concurrently.
func Compare(ctx context.Context, c Collector, listings []gazetteer.Listing, opts ...Options) Comparison {
	dossiers := make([]gazetteer.Dossier, len(listings))
	sem := make(chan struct{}, maxParallelListings)
	var wg sync.WaitGroup
	for i := range listings {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			dossiers[i] = c.Collect(ctx, listings[i])
		}(i)
	}
	wg.Wait()

	entries := make([]ComparisonEntry, len(listings))
	for i, d := range dossiers {
		price := appraisal.PricePerM2(d)
		rent := appraisal.RentValue(d)
		e := ComparisonEntry{
			Listing: listings[i],
			Dossier: d,
			Score:   Compute(d, opts...),
		}
		if price.EurPerM2Cents > 0 {
			e.PriceEURPerM2 = float64(price.EurPerM2Cents) / 100
		}
		if rent.EurPerM2Cents > 0 {
			e.RentEURPerM2 = float64(rent.EurPerM2Cents) / 100
		}
		if e.PriceEURPerM2 > 0 && e.RentEURPerM2 > 0 {
			e.YieldPct = stats.Round(e.RentEURPerM2*12/e.PriceEURPerM2*100, 1)
		}
		entries[i] = e
	}

	// Rank: candidates with a KNOWN yield (the rendement axis present) come
	// first — a yield-first comparison must not let a zone with no yield data
	// outrank one whose yield is known, however mediocre. Within each group,
	// higher composite, then higher yield, then address (deterministic).
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := hasRendement(entries[i]), hasRendement(entries[j])
		if ri != rj {
			return ri // present (true) sorts first
		}
		if entries[i].Score.Composite != entries[j].Score.Composite {
			return entries[i].Score.Composite > entries[j].Score.Composite
		}
		if entries[i].YieldPct != entries[j].YieldPct {
			return entries[i].YieldPct > entries[j].YieldPct
		}
		return entries[i].Listing.Address < entries[j].Listing.Address
	})
	for i := range entries {
		entries[i].Rank = i + 1
	}
	return Comparison{Entries: entries}
}

// hasRendement reports whether the entry's dominant yield axis was scorable.
func hasRendement(e ComparisonEntry) bool {
	for _, a := range e.Score.Axes {
		if a.Name == AxisRendement {
			return a.Present
		}
	}
	return false
}

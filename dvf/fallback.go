package dvf

// DVF's progressive zoom-out, expressed as a typed fallback ladder.
//
// The DVF chain has 4 rungs. Each rung consumes the SAME mutation
// fetcher (fetchMutationsForCommunes + FilterMutations); they differ
// only in the set of INSEE codes they query AND, for the first rung,
// in an extra geographic post-filter around the auction's geocoded
// (lat, lon):
//
//  1. address_radius — primary INSEE, then keep mutations within a
//     DVFAddressRadiusMeters disk around (auction.lat, auction.lon).
//     SkipOn'd to commune when (lat, lon) is missing OR fewer than
//     MinSampleSizeAddressRadius mutations survive the radius cut.
//     The radius is clipped to the primary commune's cadastral
//     sections — cross-border sales (auction near an INSEE boundary)
//     are NOT fetched.
//  2. commune       — the auction's resolved INSEE.
//  3. neighborhood  — communes within 5 km of the primary
//     (gazetteer.communes.Neighbors).
//  4. department    — every commune sharing the primary's department.
//
// We do NOT model "primary endpoint vs. backup endpoint" tiers here:
// DVF has a single API (dvf-api.data.gouv.fr) and HTTP-level retries
// already live inside httpx.Client. The ladder captures the GEOGRAPHIC
// fallback that was previously a `for i, communesINSEE := range levels`
// loop in Enrich — same behavior, declarative shape, structured logs.

import (
	"context"
	"log/slog"
	"time"

	"github.com/bpineau/gazetteer/pkg/communes"
	"github.com/bpineau/gazetteer/pkg/fallback"
)

// tierContext bundles the per-Query inputs that every DVF tier closes
// over. Source.compute constructs one of these at the start of Query,
// builds the ladder, and walks it.
type tierContext struct {
	target string    // type_local filter, e.g. "Appartement"
	cutoff time.Time // filter date floor (now - CutoffYears)

	// Auction-level inputs the address_radius tier needs. listingID is
	// only carried for log/telemetry context; auctionLat/auctionLon
	// are the post-filter anchor — when either is nil the radius tier
	// returns sample=0 so SkipOn falls through to commune.
	listingID  string
	auctionLat *float64
	auctionLon *float64

	// Filled in by the winning tier so the caller can shape methodParams
	// + p25/p75 quartiles without re-running the fan-out. Pointers
	// because each Try writes them; only the last (winning) write
	// matters.
	totalRaw        *int
	sectionsQueried *int
	communesQueried *[]string
	filtered        *[]Mutation
	radiusM         *float64 // set only by the address_radius tier
}

// buildLadder returns the 4-tier DVF ladder for the given INSEE.
//
// The ladder is rebuilt for every Query call because each tier closes
// over per-call state (target type_local, cutoff date, INSEE list,
// scratch counters for methodParams). The cost is negligible — four
// struct allocations.
func (s *Source) buildLadder(insee string, tc *tierContext) []fallback.Tier {
	// SkipOn for the commune / neighborhood rungs: zoom out when the
	// post-filter sample is below MinSampleSize. The address_radius
	// rung uses a slightly tighter floor (MinSampleSizeAddressRadius)
	// since its disk pulls fewer mutations than a full-commune
	// fan-out. The last rung (department) is unguarded so a hopeless
	// query still yields a low-confidence payload rather than a hard
	// error.
	belowMin := func(o fallback.Output) bool { return o.SampleSize < MinSampleSize }
	belowMinRadius := func(o fallback.Output) bool { return o.SampleSize < MinSampleSizeAddressRadius }

	primary := []string{insee}
	neighbors := s.communes.Neighbors(insee, 5.0)
	department := s.communes.SameDepartment(insee)

	return []fallback.Tier{
		{
			Name:        "address_radius",
			Description: "DVF Etalab — primary INSEE fan-out, post-filtered to a 500 m disk around (auction.lat, auction.lon)",
			Try:         s.makeTryAddressRadius(primary, tc),
			SkipOn:      belowMinRadius,
		},
		{
			Name:        "commune",
			Description: "DVF Etalab — primary INSEE only",
			Try:         s.makeTryLevel(primary, tc, "commune"),
			SkipOn:      belowMin,
		},
		{
			Name:        "neighborhood",
			Description: "DVF Etalab — communes within 5 km of primary",
			Try:         s.makeTryLevel(neighbors, tc, "neighborhood"),
			SkipOn:      belowMin,
		},
		{
			Name:        "department",
			Description: "DVF Etalab — every commune in primary's department",
			Try:         s.makeTryLevel(department, tc, "department"),
			// Last rung — accept any non-error result, even sub-min /
			// zero-sample, so the enricher returns a low-confidence
			// payload rather than a hard error. PickConfidence will
			// downgrade to "low" for callers.
			SkipOn: nil,
		},
	}
}

// makeTryLevel builds a fallback.Tier.Try closure that fetches mutations
// for `communesINSEE`, filters them, and converts the result into a
// fallback.Output. Side-effect: writes the post-filter sample size and
// the raw mutation count back into tc so Query can shape methodParams
// without re-running the fan-out.
func (s *Source) makeTryLevel(communesINSEE []string, tc *tierContext, levelName string) func(ctx context.Context, in fallback.Input) (fallback.Output, error) {
	return func(ctx context.Context, _ fallback.Input) (fallback.Output, error) {
		if err := ctx.Err(); err != nil {
			return fallback.Output{}, err
		}
		muts, secCount := s.fetchMutationsForCommunes(ctx, communesINSEE)
		filtered := FilterMutations(muts, tc.target, tc.cutoff)

		// Persist the scratch counters so Query's methodParams reflect
		// the WINNING tier's fan-out — not the cumulative effort across
		// every tier. (Each tier overwrites; only the last write — the
		// winner — matters.)
		*tc.totalRaw = len(muts)
		*tc.sectionsQueried = secCount
		*tc.communesQueried = communesINSEE
		*tc.filtered = filtered
		*tc.radiusM = 0

		_, p50, _ := PerM2Quartiles(filtered)
		var perM2 int64
		if p50 > 0 {
			perM2 = int64(p50 * 100)
		}
		return fallback.Output{
			EurPerM2Cents: perM2,
			LevelUsed:     levelName,
			SampleSize:    len(filtered),
		}, nil
	}
}

// makeTryAddressRadius builds the address_radius tier's Try closure.
//
// Behavior:
//   - When auctionLat/Lon is nil, returns SampleSize=0 with LevelUsed
//     "address_radius" and no error so SkipOn fires cleanly and the
//     ladder falls through to the commune tier.
//   - Otherwise fetches the primary commune's mutations (same fan-out
//     as the commune tier), then post-filters by HaversineKm ≤
//     DVFAddressRadiusMeters / 1000.
//   - When the tier wins (i.e. clears MinSampleSizeAddressRadius),
//     emits a Debug telemetry line including the commune-tier p50 for
//     comparison so the operator can spot tier-vs-tier divergence in
//     the logs without re-fetching.
//
// The commune-tier p50 is computed from the SAME mutation pool the
// radius post-filter operates on (no extra API fan-out).
func (s *Source) makeTryAddressRadius(communesINSEE []string, tc *tierContext) func(ctx context.Context, in fallback.Input) (fallback.Output, error) {
	return func(ctx context.Context, _ fallback.Input) (fallback.Output, error) {
		if err := ctx.Err(); err != nil {
			return fallback.Output{}, err
		}
		if tc.auctionLat == nil || tc.auctionLon == nil {
			// SkipOn-friendly: zero sample, no error, no log noise.
			// The commune tier will pick up the work.
			return fallback.Output{
				LevelUsed:  "address_radius",
				SampleSize: 0,
			}, nil
		}

		muts, secCount := s.fetchMutationsForCommunes(ctx, communesINSEE)
		communeFiltered := FilterMutations(muts, tc.target, tc.cutoff)

		// Pre-compute the commune-tier median from the same pool, for
		// telemetry. Cheap (single Quartiles call); avoids a second
		// fan-out just to label the log line.
		_, communeP50, _ := PerM2Quartiles(communeFiltered)

		radiusKm := DVFAddressRadiusMeters / 1000.0
		filtered := make([]Mutation, 0, len(communeFiltered))
		for _, m := range communeFiltered {
			if m.Latitude == nil || m.Longitude == nil {
				continue
			}
			if communes.HaversineKm(*tc.auctionLat, *tc.auctionLon, *m.Latitude, *m.Longitude) <= radiusKm {
				filtered = append(filtered, m)
			}
		}

		// Persist the scratch counters so Query's methodParams reflect
		// the radius tier's fan-out + post-filter. Overwritten by any
		// later tier that wins.
		*tc.totalRaw = len(muts)
		*tc.sectionsQueried = secCount
		*tc.communesQueried = communesINSEE
		*tc.filtered = filtered
		*tc.radiusM = DVFAddressRadiusMeters

		p25, p50, p75 := PerM2Quartiles(filtered)
		var perM2 int64
		if p50 > 0 {
			perM2 = int64(p50 * 100)
		}

		if len(filtered) >= MinSampleSizeAddressRadius {
			s.logger().Debug("dvf.address_radius_won",
				slog.String("listing_id", tc.listingID),
				slog.String("insee", communesINSEE[0]),
				slog.Int("sample", len(filtered)),
				slog.Int("n_unique_parcelles", CountUniqueParcelles(filtered)),
				slog.Float64("p25", p25),
				slog.Float64("p50", p50),
				slog.Float64("p75", p75),
				slog.Float64("commune_p50_for_comparison", communeP50),
			)
		}

		return fallback.Output{
			EurPerM2Cents: perM2,
			LevelUsed:     "address_radius",
			SampleSize:    len(filtered),
		}, nil
	}
}

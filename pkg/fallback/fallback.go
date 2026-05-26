package fallback

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Tier is one rung in an enricher's fallback ladder. The runner tries
// Tiers in order; first one that succeeds (and whose output is not
// filtered by SkipOn) wins.
type Tier struct {
	// Name is a stable short identifier (e.g. "primary", "backup_v2",
	// "department_avg"). Used as the value of the `tier` slog field so
	// dashboards can group on it.
	Name string

	// Description is a human-readable one-liner shown in the slog event.
	// Optional but recommended.
	Description string

	// Try executes the tier. Returning a non-nil error moves the runner
	// to the next tier. Returning nil + an Output that satisfies SkipOn
	// also moves to the next tier (see SkipOn).
	Try func(ctx context.Context, in Input) (Output, error)

	// SkipOn is checked AFTER Try returns successfully. If SkipOn
	// returns true the tier is treated as a soft miss and the runner
	// moves on. SkipOn may be nil — equivalent to "any successful
	// output is acceptable".
	SkipOn func(out Output) bool
}

// Input is the canonical address-shaped input shared by every ladder.
// Tier-specific extras (cadastral section, INSEE code, geocoder result)
// must be captured by closures at ladder-construction time, not added
// to this struct — Walk is purely an orchestration helper.
type Input struct {
	Address string
	City    string
	Zip     string
	Lat     *float64
	Lon     *float64
}

// Output is the canonical numeric result. Each enricher maps this back
// to its native `EnrichPayload` shape inside `Enrich`.
type Output struct {
	// EurPerM2Cents is the median €/m² in centimes. 0 means "no signal";
	// SkipOn typically tests `SampleSize == 0` rather than this field
	// because a tier may legitimately compute a 0 €/m² in pathological
	// cases.
	EurPerM2Cents int64

	// LevelUsed records which granularity produced the value:
	// "address" / "street" / "city" / "department". Stored verbatim in
	// the enricher's methodParams blob.
	LevelUsed string

	// SampleSize is the number of underlying transactions / listings
	// behind EurPerM2Cents. Most SkipOn predicates test this.
	SampleSize int

	// Source records WHICH tier produced this Output. Walk fills it in
	// from the matching Tier.Name when returning.
	Source string

	// PartialErr conveys soft errors (e.g. retry-budget reached on a
	// secondary commune in a fan-out tier) that did NOT prevent the
	// tier from returning a usable Output. Non-fatal — the runner
	// preserves it.
	PartialErr error
}

// ErrNoTierSucceeded is returned by Walk when every tier in the ladder
// either errored or was filtered out by its SkipOn predicate. The
// returned error wraps each tier's error via errors.Join, so callers
// can use `errors.Is` against any well-known sentinel.
var ErrNoTierSucceeded = errors.New("fallback: no tier succeeded")

// Walk runs each tier in order; returns the first non-skipped success.
//
// On every attempt it emits a structured slog event:
//
//	level=DEBUG msg="enrich.fallback.tier"
//	  tier=<Tier.Name>
//	  desc=<Tier.Description>
//	  outcome=<"ok"|"skip"|"err">
//	  dur_ms=<float>
//	  err=<error string, only when outcome="err">
//
// `outcome=ok` means the tier returned a non-skipped Output and Walk
// returned. `outcome=skip` means SkipOn fired. `outcome=err` means Try
// returned a non-nil error.
//
// When every tier fails or is skipped, Walk returns a zero Output and
// an error built from ErrNoTierSucceeded plus the joined per-tier
// errors.
func Walk(ctx context.Context, logger *slog.Logger, ladder []Tier, in Input) (Output, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(ladder) == 0 {
		return Output{}, fmt.Errorf("%w: empty ladder", ErrNoTierSucceeded)
	}

	var collected []error
	for _, t := range ladder {
		if err := ctx.Err(); err != nil {
			return Output{}, err
		}
		start := time.Now()
		out, err := t.Try(ctx, in)
		dur := time.Since(start)

		if err != nil {
			logger.Debug("enrich.fallback.tier",
				slog.String("tier", t.Name),
				slog.String("desc", t.Description),
				slog.String("outcome", "err"),
				slog.Float64("dur_ms", float64(dur.Microseconds())/1000.0),
				slog.Any("err", err),
			)
			collected = append(collected, fmt.Errorf("tier %q: %w", t.Name, err))
			continue
		}

		if t.SkipOn != nil && t.SkipOn(out) {
			logger.Debug("enrich.fallback.tier",
				slog.String("tier", t.Name),
				slog.String("desc", t.Description),
				slog.String("outcome", "skip"),
				slog.Int("sample_size", out.SampleSize),
				slog.Float64("dur_ms", float64(dur.Microseconds())/1000.0),
			)
			collected = append(collected, fmt.Errorf("tier %q: skipped (sample_size=%d)", t.Name, out.SampleSize))
			continue
		}

		// Success — stamp the source with the winning tier's name and
		// return.
		out.Source = t.Name
		logger.Debug("enrich.fallback.tier",
			slog.String("tier", t.Name),
			slog.String("desc", t.Description),
			slog.String("outcome", "ok"),
			slog.Int("sample_size", out.SampleSize),
			slog.String("level_used", out.LevelUsed),
			slog.Float64("dur_ms", float64(dur.Microseconds())/1000.0),
		)
		return out, nil
	}

	return Output{}, fmt.Errorf("%w: %w", ErrNoTierSucceeded, errors.Join(collected...))
}

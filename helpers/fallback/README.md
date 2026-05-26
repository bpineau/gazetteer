# fallback — typed ladder of strategies

A tiny, observable orchestrator for the "try strategy A, then B, then C,
return the first usable answer" pattern. ~150 LOC of production code,
zero dependencies beyond `log/slog`. Used by every enricher in this
project.

## Why a ladder?

The pattern shows up every time you fetch a derived value (a €/m²
estimate, a geocoded location, a building identifier) from an upstream
that can fail in many ways:

- Network failure (transient).
- Anti-bot interstitial (transient, but expensive to retry).
- Successful response with a sample too small to be meaningful (e.g. a
  DVF query on a single cadastral section returning two transactions).
- Successful response that's structurally empty (the upstream blanked
  out the payload that day).

A typed `[]Tier` per consumer gives:

- one place to read the priority order;
- one structured slog event per attempt;
- one easy unit-test surface.

## Principles

- **A tier is a strategy, not a request.** Per-call retries, timeouts
  and rate-limiting belong inside the `Try` function (typically
  delegated to `pkg/httpx`). The ladder only orchestrates between
  strategies.
- **"Succeeded but useless" is a soft miss.** Use `SkipOn` to express
  "the call returned 200 but the answer is unusable" (zero-sample
  queries, blanked payloads). The runner moves on without the caller
  having to invent a sentinel error.
- **Every attempt is observable.** Walk emits one
  `enrich.fallback.tier` slog event per attempt with `tier`, `outcome`
  ("ok"/"skip"/"err"), `dur_ms`, plus the size and level on success or
  the error on failure. No need to log inside your `Try`.
- **No retries inside Walk.** If a tier needs a retry budget, it
  composes one (e.g. by handing the call to an `httpx.Client` whose
  retry middleware does the work).

## Quick start

```go
import (
    "encheridor/pkg/fallback"
    "log/slog"
)

ladder := []fallback.Tier{
    {
        Name:        "primary",
        Description: "exact-address DVF query",
        Try:         func(ctx context.Context, in fallback.Input) (fallback.Output, error) {
            return api.QueryAtAddress(ctx, in.Address)
        },
        SkipOn: func(out fallback.Output) bool { return out.SampleSize < 3 },
    },
    {
        Name:        "fallback_street",
        Description: "street-level DVF aggregation",
        Try:         func(ctx context.Context, in fallback.Input) (fallback.Output, error) {
            return api.QueryAtStreet(ctx, in.Address, in.City)
        },
    },
}

out, err := fallback.Walk(ctx, slog.Default(), ladder, fallback.Input{
    Address: "12 rue Lafayette",
    City:    "Paris",
    Zip:     "75009",
})
if err != nil {
    // ErrNoTierSucceeded if every tier errored or was skipped.
}
```

## Public API

See `go doc encheridor/pkg/fallback` for the godoc-rendered surface:

- `type Tier struct { Name, Description string; Try func; SkipOn func }`
- `type Input  struct { Address, City, Zip string; Lat, Lon *float64 }`
- `type Output struct { EurPerM2Cents int64; LevelUsed, Source string;
  SampleSize int; PartialErr error }`
- `var ErrNoTierSucceeded error`
- `func Walk(ctx, *slog.Logger, []Tier, Input) (Output, error)`

## When to reach down a layer

If your fallback isn't an address-shaped €/m² ladder, you have two
options:

1. **Tier closures capture per-tier extras.** The `Input` struct is
   intentionally narrow (address-shaped). If a tier needs a cadastral
   section, an INSEE code or a polygon, capture it in the closure that
   builds the `Try` function. The walker is just orchestration.
2. **Compose at a higher level.** If the data shape is materially
   different (e.g. you're returning a building identifier, not a
   €/m²), wrap `fallback.Walk` in a tiny adapter that maps your
   native output to/from `fallback.Output`. This is what every
   enricher in `internal/core/enrich/<name>/fallback.go` does.

The package deliberately does not provide generics on `Output`. The
canonical address-shape is the friction point worth solving — the rest
is a 5-line adapter and the slog observability stays free.

## Observability contract

Every tier attempt emits exactly one structured slog event at DEBUG:

```
msg="enrich.fallback.tier"
  tier=<Tier.Name>
  desc=<Tier.Description>
  outcome=<"ok"|"skip"|"err">
  dur_ms=<float>
  // on success:
  sample_size=<int>
  level_used=<string>
  // on err:
  err=<string>
```

Group by `tier` in your dashboards to see relative success rates per
strategy across enrichers.

## Status

Stable. Public API frozen for the duration of the library-extraction
chantier (`doc/specs/library_extraction_plan.md` §2.6).

# Gazetteer API Review — 2026-05-26

## Context

The gazetteer library was extracted from encheridor in 2026-05 to host
the cross-cutting logic (HTTP plumbing, BAN cascade, ladder fallback,
circuit breakers, INSEE normalisation) plus 11 in-tree sources (DVF,
OSM, ADEME, BDNB, Geo­risques, Loc­service, Carte­loyers, Encadrement,
Filosofi, Taxe­fonciere, Vacance). A sibling repo,
`gazetteer-fr-plugins`, holds 4 out-of-tree antibot scrapers
(licitorweb, castorus, bienici, pappersimmo). Encheridor ships 16
adapter packages under `internal/core/enrich/` that thin-wrap these
Sources, mostly to translate sentinels and synthesise an
`EnrichPayload` envelope.

The API has now been consumed long enough by both in-tree sources and
out-of-tree plugins, with encheridor as the only end-user, that
friction patterns are visible. This document inventories the
strengths, the friction signals from consumers, and proposes a
3-tier improvement plan (non-breaking shipped now, non-breaking later,
breaking-for-v0.2).

## Strengths (keep)

- **`Listing` as the universal input**. The pointer-for-nil semantics
  (`Lat *float64`, `SurfaceM2 *float64`) cleanly disambiguates "absent"
  from "zero" — `Source.Query` implementations can rely on
  `if l.SurfaceM2 == nil` instead of inventing per-field sentinels.
  This has held across all 16 adapters.
- **`Source.Query(ctx, listing) (any, error)`**. Returning `any`
  trades a tiny amount of type safety for a single registration
  contract that lets `Dossier` JSON-roundtrip arbitrary payloads via a
  registered factory. Every Source has one typed payload struct; no
  Source has needed multi-shape returns.
- **Five-Sentinel error taxonomy + `Status` classifier**.
  (`ErrInsufficientInputs`, `ErrUnsupportedPropertyType`, `ErrAntiBot`,
  `ErrUpstreamUnavailable`, `ErrUpstreamSchemaChanged`, `ErrUpstreamPermanent`)
  Each Source wraps these and `classifyErr` in `gazetteer.go:183` maps
  to `Status` cleanly. Operators dashboarding off `Status.String()`
  works without per-source casing.
- **`EmptyReporter` opt-in**. Implementing `IsEmpty()` is optional,
  which is the right default — sources whose payload has no
  obvious "empty" concept don't have to invent one. The framework
  promotes to `StatusOKEmpty` automatically when present.
- **Context-propagated infrastructure**. `WithHTTPClient`,
  `WithLogger`, `WithDebugDump`, `WithCache` (all in `context.go`)
  keep `Source.Query` signature minimal. The fallback to
  `http.DefaultClient` / `slog.Default()` makes the Builder's
  "zero-value-works" goal achievable.
- **`Get[T](dossier, name)` generic accessor**. Replaces N per-source
  `From()` helpers conceptually (though both still exist; see
  friction §A2 below). The generic version is more discoverable from
  `gazetteer` itself; the typed `From` is more ergonomic when the
  caller already imports the source package.
- **`Result` envelope**. `Name / Version / Status / InputHash /
  FetchedAt / Err / Data` is the right minimal envelope. The custom
  `MarshalJSON` cleanly serialises Err to a string and delegates Data
  to its own marshaler.
- **Status-classified failures are actionable**. The split between
  `StatusFailedAntiBot`, `StatusFailedOutdated`, `StatusFailedPermanent`
  is genuinely useful: the encheridor doctor can route each to a
  different alert thread.
- **`circuit.TransportCircuit` shared atomic.Bool pattern**. Every
  Source that crosses a scrape boundary uses the same shape:
  `CircuitTripped *atomic.Bool` on Options, `circuit.NewTransport­Circuit`
  wired around `*httpx.Client`. Process-scoped, fresh-run-fresh,
  shared with the caller's scheduler. This works.

## Friction signals (from consumers)

### F1. Two Cache interfaces coexist (the structural duplication)

`gazetteer.Cache` (`gazetteer/cache.go:17`) and `kvcache.Cache`
(`helpers/kvcache/kvcache.go:56`) are both shipped, with different
shapes:

- `gazetteer.Cache.Get` returns `(value, hit, err)`; TTL is a
  `time.Duration` arg on `Set`.
- `kvcache.Cache.Get` returns `(Entry, err)` with `ErrNotFound`
  sentinel and pointer `ExpiresAt`; TTL is set via `kvcache.WithTTL`
  options or by populating `Entry.ExpiresAt` directly.

DVF source uses `kvcache.Cache` directly (`sources/dvf/source.go:98`).
banx uses `kvcache.Cache` directly (`helpers/banx/cache.go:81`).
bienici plugin uses `gazetteer.Cache`
(`gazetteer-fr-plugins/bienici/source.go:129`).

Consequence: encheridor ships
`internal/core/enrich/bienici/kvcache_adapter.go` (74 LOC), a thin
bridge that turns its kvcache backend into a `gazetteer.Cache`
implementation. The other 15 adapters don't need it because their
plugin consumes `kvcache.Cache` directly. The result is one custom
bridge per "Source consumes gazetteer.Cache" decision, perpetuating
the duplication.

### F2. `From(d)` and `Get[T]()` are redundant

Every shipped Source exports a `From(d gazetteer.Dossier) (*Result, bool)`
helper that boils down to `gazetteer.Get[*Result](d, Name)` (e.g.
`sources/dvf/source.go:477`, `sources/osm/source.go:198`, all four
plugins). The two-line helper is convenient when the caller already
imports the source package, but it's a stable wire contract every
source now has to maintain. Removing it isn't backwards-compatible
(encheridor uses it everywhere), but new sources can be steered to
skip it once `Get[T]()` is the canonical pattern.

### F3. `Evidence` sidecar is a typed first-class slot in practice

Every shipped Source has the same shape: typed `Result` carries an
`Evidence` field (tagged `json:"-"`) with reproducibility metadata.
Encheridor's adapters route Evidence into
`EnrichPayload.Method.Params`. The pattern emerged organically but
isn't surfaced in `gazetteer.Result`. A consumer reading
`gazetteer/result.go` won't know it exists.

This isn't a bug — keeping Evidence opaque on the framework lets
each Source pick its own shape — but the convention is now strong
enough to deserve a docstring note in `source.go` or `result.go`,
and possibly a typed `Evidencer` opt-in interface mirroring
`EmptyReporter`.

### F4. Setup ergonomics: README example panics on first call

The README's quick-start (`README.md:55-62`) writes:

```go
client, err := gazetteer.NewBuilder().
    With(dvf.NewSource(dvf.Options{})).
    With(osm.NewSource(osm.Options{})).
    Build()
```

`dvf.NewSource(dvf.Options{})` panics with `"dvf.NewSource: nil HTTP
client"` (`sources/dvf/source.go:130`). The README hasn't been kept in
sync with the post-port `Options` mandatory fields. `osm` accepts
`Options{}` (and returns `ErrNoCatalog` until `UpdateCatalog` is
called) — `dvf` does not.

### F5. Inconsistent NewSource construction failure modes

Three flavors coexist:

- **Panic on missing dep** — dvf, bienici, licitorweb panic when
  required Options fields are nil.
- **Error return** — pappersimmo returns `(*Source, error)`.
- **Always-succeeds** — ademe, osm, bdnb, georisques, locservice
  succeed on zero-value Options and lazy-resolve at Query time.

The inconsistency makes the "what does NewSource do?" question
unanswerable without reading the source. Pappersimmo's choice
(return error) is the only one that composes well with
`gazetteer.NewBuilder()`: if `NewSource` panics, the Builder
construction can't bail gracefully.

### F6. `BaseURL` test override is per-Source, ad-hoc

pappersimmo has `Options.BaseURL` for test override (see
`pappersimmo/source.go:124`); bdnb, ademe, georisques, locservice,
encadrement, filosofi, taxefonciere, vacance, carteloyers all have
their own `BaseURL` (or equivalent) field. Plugins (bienici,
castorus, licitorweb) do NOT have `BaseURL` — their URLs are
hard-coded in `*_url.go`. The result is a non-uniform contract for
the very common "point this Source at httptest.NewServer.URL" test
pattern.

### F7. `Source.QueryWithAdvertID` is a generalisation hint

licitorweb has `Source.QueryWithAdvertID(ctx, l, advertID)`
(`gazetteer-fr-plugins/licitorweb/source.go:360`) — a side entry
point that bypasses the `Options.AdvertIDFor` hook. Encheridor's
adapter calls it (`internal/core/enrich/licitorweb/licitorweb.go:249`)
because the advert id is fetched from a separate table and the
hook closure would have to re-do the lookup.

This is a generalisation hint: more Sources may need "extra-arg
passing" beyond what Listing carries. The framework currently has no
formal mechanism, leaving each Source to invent its own.

### F8. `ErrCircuitTripped` is a near-canonical sentinel

Six packages currently export their own `ErrCircuitTripped`:
licitorweb, pappersimmo, bienici, castorus (plugins) plus dvf
(in-tree) and the encheridor adapters. The error message differs
only by source name prefix; the runner uses `errors.Is(err,
ErrCircuitTripped)` to detect. A canonical `gazetteer.ErrSourceCircuit­Tripped`
would let plugins wrap once instead of declaring their own, and would
let cross-source diagnostics (encheridor doctor) match on a single
sentinel.

### F9. `Options.HTTPClient` vs `HTTPClientFrom(ctx)` — both used, semantics murky

Three patterns exist:

- **Required `*httpx.Client`**: dvf, pappersimmo. Construction
  fails if absent.
- **Optional `*http.Client`, fallback to `gazetteer.HTTPClientFrom(ctx)`**:
  ademe, georisques, locservice, bdnb, encadrement, taxefonciere,
  carteloyers, vacance, castorus, licitorweb, bienici.
- **No HTTPClient at all** (uses `gazetteer.HTTPClientFrom(ctx)` always):
  filosofi.

Pattern 2 is by far the most common but the "when do I use Options
vs ctx?" rule isn't documented. `Builder.WithHTTPClient` propagates
through ctx, but a Source can still override via Options — and that
override silently wins. Worth a docstring on the convention.

### F10. `SetDefaultNormalizer` is a global side-effect

`gazetteer.SetDefaultNormalizer` (`normalize.go:35`) writes to a
package-private `var`. `gazetteer.NormalizeAddress` reads from it.
This is fine for one-shot tools but two test suites running in
parallel can step on each other. The Builder accepts a per-builder
Normalizer via `WithNormalizer` (`gazetteer.go:78`), but it's
explicitly noted as "currently unused by the Builder itself".

There's no consumer of the per-Builder Normalizer today: every
caller goes through the package-level facade. Either remove
`WithNormalizer` (it's dead code) or wire it through to a
`Client.Normalize` method so the global goes away.

### F11. Setup boilerplate per session

Wiring a working client from zero looks like:

```go
hc, _ := httpx.New(httpx.Options{})
gazetteer.SetDefaultNormalizer(
    gazetteer.NewBANNormalizer(banx.NewBANClient(hc), communes.MustDefault()),
)
geocoder := banx.NewBANClient(hc)
client, _ := gazetteer.NewBuilder().
    With(dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: geocoder, ...})).
    With(bienici.NewSource(bienici.Options{HTTPClient: hc.HTTPClient(), Cache: ...})).
    With(pappersimmo.NewSource(...)).
    Build()
```

A "sensible defaults" helper — `gazetteer.NewDefault(ctx) (*Client,
error)` that returns a client wired with all stable sources +
httpx + BAN — would shrink the README example from 40 LOC to 5
and make the lib usable as an out-of-the-box CLI dependency. (The
existing `cmd/gazetteer` already does this assembly internally;
exposing it as a top-level package function would close the loop.)

### F12. `Status` is an iota int, not a string type

`Status` is `int`; `Status.String()` is a switch. Marshaled as a
string via `Result.MarshalJSON` (`result.go:36`). Round-trips via
`parseStatus` (`dossier.go:110`). All wire consumers see the string
form. Defining `Status` as a typed `string` const set would:
- Remove the dual marshaling/parsing path
- Let `Status` be safely used as a map key without ambiguity about
  whether you're using ordinal or label

This is technically breaking for anyone who switches on the int
constants — for in-repo usage, almost no one does (they switch on
`StatusOK == s` which still works with a string type).

### F13. No godoc Example tests for the plugin pattern

`gazetteer/example_test.go` shows the `Builder.Collect + Get[T]`
path beautifully. There's no Example for:
- The Source interface (how to implement a custom Source)
- The Normalizer + Builder flow
- The plugin registration pattern (`init() { Register(...) }`)
- Reading from a JSON-roundtripped Dossier (`UnmarshalJSON`)

These would all be 15-line examples that solve real "how do I…"
questions.

## Proposed improvements

### Non-breaking (shipped in this review)

The shipped commit SHAs are filled in at the end of the review
session.

- **N1. Fix README quick-start.** Update `dvf.NewSource(dvf.Options{})`
  to construct with mandatory HTTP+Geocoder so the example
  actually compiles + runs. Add a short note that `osm.NewSource(osm.Options{})`
  needs `UpdateCatalog` before serving.
- **N2. Add `kvcache.GazetteerAdapter`.** A `helpers/kvcache.NewGazetteerAdapter(kvcache.Cache) gazetteer.Cache`
  function. Collapses the 74-LOC `kvcache_adapter.go` in encheridor
  into a one-line `bienici.Options{Cache: kvcache.NewGazetteerAdapter(myCache)}`
  on the caller side. Pure addition, no existing behaviour changes.
- **N3. Document Evidence convention.** Add a docstring paragraph to
  `gazetteer.Result.Data` noting that the by-convention typed Result
  often carries a typed `Evidence` field; surface the recommended
  shape so new Sources follow the pattern instead of reinventing it.
- **N4. Document `Options.HTTPClient` vs `HTTPClientFrom(ctx)`
  precedence rule.** Add a paragraph to `gazetteer.NewBuilder` and
  to `context.go::HTTPClientFrom` explaining the convention.
- **N5. Plugin source pattern documentation.** Add a godoc Example
  in `gazetteer/example_test.go` showing how to implement a custom
  Source + `Register` it.
- **N6. Add `gazetteer.NewMemCache` shorthand for kvcache backends.**
  Add `gazetteer.NewKVMemCache()` returning `kvcache.Cache` so
  callers don't need to import `memcache` separately. (Optional —
  this is a really tiny convenience.)

### Non-breaking (for next iteration)

- **L1. `ErrSourceCircuitTripped` canonical sentinel.** A new
  `gazetteer.ErrSourceCircuitTripped` that source-local
  `ErrCircuitTripped` values wrap. Sources keep their per-package
  sentinel for backwards-compat; callers gain a unified
  `errors.Is(err, gazetteer.ErrSourceCircuitTripped)` check.
- **L2. `gazetteer.NewDefault(ctx)` factory.** A top-level helper
  that wires httpx + BAN + every in-tree Source with sensible
  defaults. Reduces a 40-LOC README example to 5 LOC.
- **L3. `gazetteer/gazettestest` package with `StubSource`.** A
  test-only helper that builds a `Source` from a fixed `(payload,
  err)` pair. Eliminates the ad-hoc `fakeSource` definitions
  scattered across consumer test files.
- **L4. Add `BaseURL` to plugins (bienici, castorus, licitorweb).**
  Mirrors the in-tree Sources' BaseURL convention. Plugins
  currently lack it, which means race-conditioned tests have to
  go through `http.Client.Transport` hacking (see the pappersimmo
  race-fix story in encheridor history).
- **L5. `Source.QueryWith(ctx, l, args ...any)` extra-arg passing.**
  Generalisation of `QueryWithAdvertID`. A typed `SourceArgs[T]`
  generic interface or simple `any` slot. Optional — keeps base
  `Source.Query` unchanged, adds a richer call path for sources
  that need it (licitorweb's advert id pattern is the precedent).
- **L6. `NewSource` should never panic.** Audit dvf, bienici,
  licitorweb to return `(*Source, error)` for required-dep checks.
  Keeps `Builder.With` chain-friendly only as long as the user
  ignores the error, but the same is true for pappersimmo today.
- **L7. Make `EmptyReporter` discoverable via Result method.**
  `Result.IsEmpty() bool` proxying to `Data.(EmptyReporter).IsEmpty()`
  saves callers from type-asserting on Dossier consumers. Pure
  addition, no behaviour change.

### Breaking (for a v0.2)

- **B1. Unify `Cache` interfaces.** Drop `gazetteer.Cache`; use
  `kvcache.Cache` everywhere. Sources currently consuming
  `gazetteer.Cache` (bienici) migrate to `kvcache.Cache`. Encheridor's
  `kvcache_adapter.go` becomes empty. Two interfaces collapse to one.
  Mechanically breaking for bienici's `Options.Cache` field type.
- **B2. Promote `Evidence` to typed first-class slot.** Add
  `Result.Evidence any` (analogue to `Result.Data`). Sources still
  free to implement an `Evidencer` interface to populate it; the
  framework records the slot uniformly. Lets consumers ask
  `dossier.Results["dvf"].Evidence` without reading the typed
  Data through reflection.
- **B3. `Status` as typed string.** Replace `type Status int` +
  `String()/parseStatus` with `type Status string`. Constants become
  string-typed (`StatusOK Status = "ok"`). Eliminates the dual
  marshaling path; better debuggability.
- **B4. Remove `SetDefaultNormalizer` global.** Either remove
  `Builder.WithNormalizer` (it's dead code today) or wire it
  through and remove the package-level facade. Pick one.
- **B5. Standardise `From(d) → Get[T](d, Name)` migration.** Either
  drop per-source `From()` helpers entirely (callers switch to
  `gazetteer.Get[*Result](d, "dvf")`) or document `From` as the
  canonical surface and remove `Get`. Both can't be the right
  answer.
- **B6. Per-Source `BaseURL` standardised in Options shape.** Add
  `BaseURL string` to the framework's `Source` contract via a
  `BaseURLer` opt-in interface, or via an `Options` struct
  embedded in every Options. Mostly cosmetic but tightens
  the testing story across all 15 Sources.

## Conclusion

The lib is in a healthy alpha. The Source contract has held across
15 Sources + 4 plugins without major friction; the
`Status`/sentinel/`EmptyReporter` design has proven itself in
production. The next batch of improvements is mostly about
collapsing duplications (two Caches, redundant `From`/`Get`, per-source
`ErrCircuitTripped`) and surfacing patterns that emerged organically
(Evidence, `QueryWith` extra args, BaseURL).

Recommended immediate ship: **N1–N6** (this review). Recommended for
the next chantier on gazetteer: **L1, L2, L3** (canonical circuit
sentinel + sensible-defaults factory + test stub package). Defer the
breaking changes until there is a v0.2 stabilisation milestone with
encheridor on a versioned dependency rather than a `replace` directive.

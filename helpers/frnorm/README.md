# frnorm — French free-text normalizers

Side-effect-free helpers for parsing the shapes that turn up everywhere in
French free-form text: prices, dates, postcodes, accents, whitespace and
hearing-times. Pure functions, no I/O, allocation-bounded, safe for
concurrent use.

## Why this package exists

Multiple HTML / PDF scrapers can independently re-derive the same regex
(`\d{5}` for a postcode, "is this a French month name?", "what is
`1.336.500,50 €` in cents?"). The copies drift: a fix lands in one parser
and regresses in another. `frnorm` is the one place those helpers live;
every consumer imports it.

## Principles

- **Lexical only.** No I/O, no DB, no auction vocabulary. Anything you
  hand in is a `string`; anything you get back is a `string`, an `int64`,
  or a `time.Month`. Geocoding, INSEE lookup, currency conversion all
  belong elsewhere.
- **FR conventions are first-class.** "1 234,56 €" parses; "1,234.56" is
  garbage on purpose (the Anglo convention is rejected — see
  `ParseFRPriceToCentimes` doc).
- **Accent folding preserves case.** `Étoile → Etoile`, `étoile → etoile`.
  Lower-casing is a separate concern that callers compose on top.
- **Pure.** Every function is deterministic, allocation-bounded and safe
  for concurrent calls. No package-level mutable state.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/frnorm"

cents := frnorm.ParseFRPriceToCentimes("1 336 500,50 €")
// cents == 133650050  // 1 336 500.50 €

zip, city, ok := frnorm.ExtractZipCity("75 rue Lafayette, 75009 Paris")
// zip == "75009", city == "Paris", ok == true

month, ok := frnorm.FrenchMonth("févr.")
// month == time.February, ok == true

clean := frnorm.NormaliseSpace("foo  \t bar  ")
// clean == "foo bar"

stripped := frnorm.StripAccents("L'École de l'Étoile")
// stripped == "L'Ecole de l'Etoile"

hhmm := frnorm.NormalizeHearingTime("14h30")
// hhmm == "14:30"
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/frnorm` for the
godoc-rendered surface. The exported helpers are:

- `ParseFRPriceToCentimes(string) int64` — FR-formatted money → centimes
- `ExtractZipFromAddress(string) (zip string, ok bool)`
- `ExtractZipCity(string) (zip, city string, ok bool)`
- `FindZipIndex(string) (start, end int)` — low-level, returns the
  `\d{5}` span (or -1, -1)
- `FrenchMonth(string) (time.Month, bool)` — accepts "janvier", "janv.",
  "févr", "fev.", any common abbreviation
- `StripAccents(string) string` — case-preserving Latin-1/A diacritic fold
- `NormaliseSpace(string) string` — collapse NBSP, narrow NBSP, runs of
  whitespace; trim
- `NormalizeHearingTime(string) string` — "14h30" / "14h" / "14H30 " → "14:30"

## When to reach down a layer

Reach for the stdlib (`strconv`, `time.Parse`, `regexp`) when:

- The input shape is already canonicalised (e.g. ISO-8601 dates from a
  JSON API).
- You need locale-aware handling beyond FR.
- You need streaming / very-high-throughput operation; these helpers are
  fast but not zero-allocation.

`frnorm` is for the *messy human-typed FR input* surface that arrives via
HTML scraping, PDF extraction or operator-supplied search filters.

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.

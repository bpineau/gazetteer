# proptype — canonical property-type normalisation

The single source of truth mapping raw property-type strings (any case,
any language, stray whitespace) onto the canonical enum Sources use to
gate eligibility: `apartment`, `house`, `land`, `commercial`, plus
`unknown` as the catch-all.

Scrapers, importers and APIs all spell "apartment" differently
("Appartement", "APPT", "flat", …); this package is where those
aliases live, once.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/proptype"

pt := proptype.Normalize("Appartement") // proptype.Apartment
if !pt.IsKnown() {
    // unrecognised input: treat as unknown, don't guess
}

// Nil-tolerant variant for optional columns:
pt = proptype.NormalizePtr(row.RawType) // nil -> proptype.Unknown
```

Bridging to the gazetteer Listing (the enum values match
`gazetteer.PropertyType`):

```go
if lt, ok := proptype.ToListingType(raw); ok {
    listing.PropertyType = lt
}
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/proptype`:

- `type PropertyType` (typed string; round-trips through JSON, logs and
  map keys) with `IsKnown()` and `String()`
- `const Apartment / House / Land / Commercial / Unknown`
- `func Normalize(raw string) PropertyType`
- `func NormalizePtr(raw *string) PropertyType`
- `func ToListingType(raw string) (gazetteer.PropertyType, bool)`

## Status

Stable. New aliases are added freely; the canonical values are frozen.

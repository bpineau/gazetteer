# fraddr — free-form French address parser

A tiny library that turns free-form French street addresses into a
structured `Parts` value (street number with `bis`/`ter`/`quater` and
range support, plus the most discriminating street-name tokens with
street-type markers stripped).

## Why this package exists

Every scraper, geocoder front-end and BAN-resolver shim ends up
needing the same "first comma-anchored digit, drop the street-type
word, stop at the postcode" deconstruction. `fraddr` is that shell,
shared across projects.

## Example

```go
package main

import (
    "fmt"

    "github.com/bpineau/gazetteer/helpers/fraddr"
)

func main() {
    p := fraddr.Parse("Résidence Le Méridien, 32 rue Dareau, 75014 Paris")
    fmt.Println(p.Number)       // "32"
    fmt.Println(p.StreetTokens) // [Dareau]
    fmt.Println(p.Query())      // "32 Dareau"
    fmt.Println(p.Pattern())    // "Dareau"

    fmt.Println(fraddr.IsFrPostalCode("75011")) // true
    fmt.Println(fraddr.IsFrPostalCode("7501"))  // false
}
```

## Parser pipeline

`Parse` runs the following steps in order:

1. **Comma-anchored re-start.** If the input does not begin with a
   digit, split on `,` and pick the first segment whose first non-space
   character is a digit (skips commercial / residence-name prefixes).
2. **Embedded house-number anchor.** If Step 1 didn't re-anchor, scan
   the full input for the first `<n> <street-type>` group (capped at
   4 digits to avoid eating postcodes) and re-anchor there.
3. **Strip commas** so tokenisation is whitespace-only.
4. **Stop at the postcode.** Everything from the first 5-digit token
   onward is discarded.
5. **Extract the leading house number** (first digit run; "30-32" →
   "30", "32B" → "32").
6. **Drop street-type tokens** (`rue`, `bd`, `imp.`, `chem`, …).
7. **Cap at 3 remaining tokens** — beyond 3, the query
   over-constrains.

## API

| Symbol | Purpose |
|--------|---------|
| `Parse(addr string) Parts` | The main entry point. |
| `(Parts).Query() string` | Number + street tokens, space-separated. |
| `(Parts).Pattern() string` | Street tokens only (for ILIKE patterns). |
| `IsFrPostalCode(s string) bool` | Heuristic 5-ASCII-digits check. |
| `ItoaPositive(n int) string` | Small `strconv.Itoa` alternative for positive `int`. |

## Dependencies

Pure standard library: `regexp` and `strings`.

# communes — French commune lookup table

A small, dependency-free Go library that ships a 35 000-row snapshot of
French communes (INSEE code, department, lon/lat centroid, name) and
exposes:

- INSEE → `Commune` lookup
- neighbor search within an N-km haversine radius
- same-department enumeration
- reverse city-name → department codes lookup
- a stand-alone `HaversineKm` helper

The dataset (~1.3 MB, `data/france.csv`) is embedded at build time via
`go:embed`, so consumers get the table from a single `Default()` call —
no file deployment, no HTTP fetch, no init() side effect until first use.

## Source

`https://geo.api.gouv.fr/communes` — snapshot fetched and stored as
`data/france.csv` with the header `insee,dept,lon,lat,name`. The
arrondissements of Paris (75101–75120), Lyon (69381–69389) and
Marseille (13201–13216) are included, each as their own row.

## Example

```go
package main

import (
    "fmt"

    "myrepo/pkg/communes"
)

func main() {
    tbl := communes.MustDefault()

    if c, ok := tbl.Lookup("75107"); ok {
        fmt.Printf("%s — dept %s — (%.4f, %.4f)\n",
            c.Name, c.Dept, c.Lat, c.Lon)
    }

    // Communes within 5 km of Paris 7e arrondissement.
    near := tbl.Neighbors("75107", 5.0)
    fmt.Println(len(near), "communes within 5 km")

    // Reverse: in which department is "Vincennes" located?
    depts := tbl.CityDepts("Vincennes")
    fmt.Println(depts) // [94]
}
```

## Test fixtures

To avoid the 1.3 MB embedded CSV in unit tests, call `NewTable(rows)`
directly with a hand-crafted slice, or feed `ParseCSV(reader)` with a
small in-memory CSV.

## API

| Symbol | Purpose |
|--------|---------|
| `Default() (*Table, error)` | Singleton parsed from the embedded CSV. |
| `MustDefault() *Table` | Same, panics on parse error. |
| `NewTable(rows []Commune) *Table` | Build a table from an explicit slice. |
| `ParseCSV(r io.Reader) (*Table, error)` | Parse a CSV with the canonical header. |
| `FranceCSVBytes() []byte` | Raw embedded bytes, for inspection. |
| `(*Table).Lookup(insee string) (Commune, bool)` | Direct INSEE lookup. |
| `(*Table).Neighbors(insee string, radiusKm float64) []string` | Haversine sweep. |
| `(*Table).SameDepartment(insee string) []string` | All INSEE codes in the dept. |
| `(*Table).CityDepts(name string) []string` | Reverse name → dept codes. |
| `HaversineKm(lat1, lon1, lat2, lon2 float64) float64` | Great-circle distance. |
| `Commune` | Record struct. |
| `Communes` | Interface for the three primary operations. |

## Dependencies

Pure standard library (`math`, `sort`, `strings`, `sync`, `unicode`,
`encoding/csv`, `embed`).

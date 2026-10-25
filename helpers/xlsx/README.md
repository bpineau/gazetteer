# xlsx: the cell grid of a workbook, standard library only

Some public-data publishers (INSEE first) ship tables as `.xlsx` only.
A transform that needs the rows of one sheet should not carry a full
spreadsheet engine, its formula evaluator and its security advisories
for that. This package reads what a published table uses, and nothing
else:

- shared strings (plain and rich text), inline strings, numbers,
  booleans, cached formula results, error cells;
- sparse rows and columns, rendered the way a spreadsheet shows them;
- every ZIP member capped at `MaxPartSize`, so a hostile file cannot
  inflate without bound.

It does not evaluate formulas, apply number formats, or read styles,
merged ranges, dates as dates, or anything outside the cell grid.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/xlsx"

wb, err := xlsx.OpenBytes(body)      // or xlsx.Open(readerAt, size)
names := wb.Sheets()                 // tab names, workbook order
rows, err := wb.Rows("txcho_ze")     // [][]string, A1 at rows[0][0]
```

`Rows` returns row i of the sheet as `rows[i-1]`; an empty row between
two filled ones is an empty slice, a skipped column is `""`, and the
trailing empty cells of a row are dropped. A number is rendered as the
shortest decimal that reads back to the same `float64` (`"7.4"`,
`"34875"`), a boolean as `"TRUE"`/`"FALSE"`.

`Encode` writes a minimal workbook (every cell an inline string) for
tests and plain exports:

```go
b, err := xlsx.Encode(xlsx.Sheet{Name: "COM", Rows: [][]string{
    {"CODGEO", "LIBGEO", "ZE2020"},
    {"01001", "L'Abergement-Clémenciat", "8405"},
}})
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/xlsx`:

- `func Open(r io.ReaderAt, size int64) (*Workbook, error)`,
  `func OpenBytes(b []byte) (*Workbook, error)`
- `func (*Workbook) Sheets() []string`,
  `func (*Workbook) Rows(sheet string) ([][]string, error)`
- `type Sheet struct { Name string; Rows [][]string }`,
  `func Encode(sheets ...Sheet) ([]byte, error)`
- `const MaxPartSize`, `var ErrNoSheet`

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.

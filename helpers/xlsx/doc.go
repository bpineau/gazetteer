// Package xlsx reads the cell grid of an Office Open XML spreadsheet
// (.xlsx) with the standard library alone, and writes a minimal one for
// fixtures and exports.
//
// It exists because the public-data publishers (INSEE first) ship some
// tables as workbooks only, and a transform that needs the rows of one
// sheet should not carry a full spreadsheet engine (styles, formulas,
// charts, drawings) and its advisories for that. The reader covers what
// a published table uses: shared and inline strings, numbers, booleans,
// cached formula results, sparse rows and columns. It does not evaluate
// formulas, apply number formats, or read styles, merged ranges, dates
// as dates, or anything outside the cell grid.
//
//	wb, err := xlsx.OpenBytes(body)
//	rows, err := wb.Rows("txcho_ze")   // [][]string, A1 at rows[0][0]
//
// Rows returns the grid the way a spreadsheet shows it: row i of the
// sheet is rows[i-1], an empty row between two filled ones is an empty
// slice, a skipped column is "", and trailing empty cells of a row are
// dropped. A numeric cell is rendered as the shortest decimal that reads
// back to the same float64 ("7.4", "34875"), never in exponent form for
// the magnitudes a table holds; a boolean as "TRUE"/"FALSE".
//
// Encode is the inverse for tests and small exports: it writes every
// cell as an inline string, which Rows reads back verbatim.
//
// A workbook is a ZIP archive, so an untrusted file can declare members
// that inflate without bound; every member read here is capped at
// MaxPartSize and a larger one is an error, not a hang.
package xlsx

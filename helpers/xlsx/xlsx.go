package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// MaxPartSize caps the uncompressed size of any single ZIP member the
// reader inflates (a worksheet, the shared-string table). INSEE's largest
// commune table is about 20 MB; a member past this cap is refused.
const MaxPartSize = 512 << 20

// ErrNoSheet is returned by Rows for a sheet name the workbook does not
// have; Sheets lists the ones it has.
var ErrNoSheet = errors.New("xlsx: no such sheet")

// Workbook is a read-only view of one .xlsx file: its sheet names and the
// cell grid of each. Open it with Open or OpenBytes.
type Workbook struct {
	zr     *zip.Reader
	parts  map[string]*zip.File
	sheets []sheetRef
	sst    []string
	sstOK  bool
	sstErr error
}

type sheetRef struct {
	name string
	part string // ZIP member path, e.g. "xl/worksheets/sheet2.xml"
}

// Open reads the workbook structure (sheet list, relationships) from r.
// Sheet contents are parsed on demand by Rows.
func Open(r io.ReaderAt, size int64) (*Workbook, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("xlsx: open archive: %w", err)
	}
	wb := &Workbook{zr: zr, parts: make(map[string]*zip.File, len(zr.File))}
	for _, f := range zr.File {
		wb.parts[strings.TrimPrefix(f.Name, "/")] = f
	}
	if err := wb.readSheetList(); err != nil {
		return nil, err
	}
	return wb, nil
}

// OpenBytes is Open over an in-memory file.
func OpenBytes(b []byte) (*Workbook, error) {
	return Open(bytes.NewReader(b), int64(len(b)))
}

// Sheets returns the sheet names in workbook order.
func (w *Workbook) Sheets() []string {
	out := make([]string, len(w.sheets))
	for i, s := range w.sheets {
		out[i] = s.name
	}
	return out
}

// Rows returns the cell grid of the named sheet; see the package
// documentation for the shape. The comparison of sheet names is exact.
func (w *Workbook) Rows(sheet string) ([][]string, error) {
	var ref *sheetRef
	for i := range w.sheets {
		if w.sheets[i].name == sheet {
			ref = &w.sheets[i]
			break
		}
	}
	if ref == nil {
		return nil, fmt.Errorf("%w: %q (have %q)", ErrNoSheet, sheet, w.Sheets())
	}
	if err := w.loadSharedStrings(); err != nil {
		return nil, err
	}
	rc, err := w.open(ref.part)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	rows, err := w.parseSheet(rc)
	if err != nil {
		return nil, fmt.Errorf("xlsx: sheet %q: %w", sheet, err)
	}
	return rows, nil
}

// open returns a size-capped reader over a ZIP member.
func (w *Workbook) open(part string) (io.ReadCloser, error) {
	f, ok := w.parts[part]
	if !ok {
		return nil, fmt.Errorf("xlsx: missing part %q", part)
	}
	if f.UncompressedSize64 > MaxPartSize {
		return nil, fmt.Errorf("xlsx: part %q declares %d bytes, above the %d cap", part, f.UncompressedSize64, MaxPartSize)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("xlsx: open part %q: %w", part, err)
	}
	return &cappedReader{ReadCloser: rc, left: MaxPartSize, part: part}, nil
}

// cappedReader errors past MaxPartSize even when the header lied.
type cappedReader struct {
	io.ReadCloser
	left int64
	part string
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, fmt.Errorf("xlsx: part %q exceeds the %d byte cap", c.part, MaxPartSize)
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.ReadCloser.Read(p)
	c.left -= int64(n)
	return n, err
}

// readSheetList pairs xl/workbook.xml's <sheet name r:id> entries with the
// relationship targets of xl/_rels/workbook.xml.rels.
func (w *Workbook) readSheetList() error {
	rels, err := w.readRels("xl/_rels/workbook.xml.rels")
	if err != nil {
		return err
	}
	rc, err := w.open("xl/workbook.xml")
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("xlsx: workbook.xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		var name, rid string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "id":
				rid = a.Value
			}
		}
		target, ok := rels[rid]
		if !ok {
			return fmt.Errorf("xlsx: sheet %q: relationship %q not found", name, rid)
		}
		w.sheets = append(w.sheets, sheetRef{name: name, part: target})
	}
	if len(w.sheets) == 0 {
		return errors.New("xlsx: workbook has no sheet")
	}
	return nil
}

// readRels parses a relationships part into Id -> resolved ZIP member
// path. Targets are relative to the part's owner directory ("xl/") unless
// they start with "/".
func (w *Workbook) readRels(part string) (map[string]string, error) {
	rc, err := w.open(part)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, fmt.Errorf("xlsx: %s: %w", part, err)
	}
	base := path.Dir(path.Dir(part)) // "xl/_rels" -> "xl"
	out := make(map[string]string, len(doc.Rels))
	for _, r := range doc.Rels {
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join(base, t)
		}
		out[r.ID] = t
	}
	return out, nil
}

// loadSharedStrings parses xl/sharedStrings.xml once. A workbook with no
// shared-string table (inline strings only) is fine.
func (w *Workbook) loadSharedStrings() error {
	if w.sstOK {
		return w.sstErr
	}
	w.sstOK = true
	if _, ok := w.parts["xl/sharedStrings.xml"]; !ok {
		return nil
	}
	rc, err := w.open("xl/sharedStrings.xml")
	if err != nil {
		w.sstErr = err
		return err
	}
	defer func() { _ = rc.Close() }()
	w.sst, w.sstErr = parseSharedStrings(rc)
	return w.sstErr
}

// parseSharedStrings concatenates the <t> runs of every <si> (plain and
// rich text alike), skipping phonetic <rPh> runs.
func parseSharedStrings(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	var (
		out     []string
		cur     strings.Builder
		inSI    bool
		inT     bool
		phonDep int
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: sharedStrings.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "rPh":
				phonDep++
			case "t":
				inT = inSI && phonDep == 0
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "rPh":
				phonDep--
			case "si":
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out, nil
}

// parseSheet streams <sheetData> into the row grid.
func (w *Workbook) parseSheet(r io.Reader) ([][]string, error) {
	dec := xml.NewDecoder(r)
	var (
		rows   [][]string
		row    []string
		rowIdx int // 1-based sheet row of `row`; 0 = none open
		colIdx int // 0-based column of the open cell
		cell   cellState
	)
	flush := func() {
		if rowIdx == 0 {
			return
		}
		for len(rows) < rowIdx-1 {
			rows = append(rows, []string{})
		}
		rows = append(rows, trimTrailing(row))
		row, rowIdx = nil, 0
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				flush()
				rowIdx = len(rows) + 1
				if v := attr(t, "r"); v != "" {
					n, err := strconv.Atoi(v)
					if err != nil || n < rowIdx {
						return nil, fmt.Errorf("row %q: bad or out-of-order index", v)
					}
					rowIdx = n
				}
			case "c":
				if rowIdx == 0 {
					return nil, errors.New("cell outside a row")
				}
				colIdx = len(row)
				if ref := attr(t, "r"); ref != "" {
					n, err := columnOf(ref)
					if err != nil {
						return nil, err
					}
					if n < colIdx {
						return nil, fmt.Errorf("cell %q: out-of-order column", ref)
					}
					colIdx = n
				}
				cell = cellState{open: true, typ: attr(t, "t")}
			case "v":
				cell.inV = cell.open
			case "t":
				cell.inT = cell.open && cell.typ == "inlineStr"
			}
		case xml.CharData:
			if cell.inV || cell.inT {
				cell.text.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				cell.inV = false
			case "t":
				cell.inT = false
			case "c":
				val, err := w.cellValue(cell)
				if err != nil {
					return nil, fmt.Errorf("row %d col %d: %w", rowIdx, colIdx+1, err)
				}
				for len(row) < colIdx {
					row = append(row, "")
				}
				row = append(row, val)
				cell = cellState{}
			case "row":
				flush()
			}
		}
	}
	flush()
	return rows, nil
}

type cellState struct {
	open bool
	typ  string
	inV  bool
	inT  bool
	text strings.Builder
}

// cellValue renders one cell as Rows shows it.
func (w *Workbook) cellValue(c cellState) (string, error) {
	s := c.text.String()
	switch c.typ {
	case "s":
		if s == "" {
			return "", nil
		}
		i, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || i < 0 || i >= len(w.sst) {
			return "", fmt.Errorf("shared string index %q out of range (%d strings)", s, len(w.sst))
		}
		return w.sst[i], nil
	case "b":
		if strings.TrimSpace(s) == "1" {
			return "TRUE", nil
		}
		return "FALSE", nil
	case "str", "inlineStr", "e":
		return s, nil
	default: // "n" or absent: a number, or empty
		s = strings.TrimSpace(s)
		if s == "" {
			return "", nil
		}
		// A value that is not a float64 is handed over verbatim: the
		// publisher's text is more useful to the caller than an error.
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		return s, nil
	}
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// columnOf converts the letters of an A1 reference to a 0-based column.
func columnOf(ref string) (int, error) {
	n := 0
	for i := 0; i < len(ref); i++ {
		ch := ref[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A'+1)
	}
	if n == 0 {
		return 0, fmt.Errorf("cell %q: no column letters", ref)
	}
	return n - 1, nil
}

// columnName is columnOf's inverse: 0 -> "A", 26 -> "AA".
func columnName(col int) string {
	var b []byte
	for n := col + 1; n > 0; n = (n - 1) / 26 {
		b = append([]byte{byte('A' + (n-1)%26)}, b...)
	}
	return string(b)
}

func trimTrailing(row []string) []string {
	n := len(row)
	for n > 0 && row[n-1] == "" {
		n--
	}
	if n == 0 {
		return []string{}
	}
	return row[:n]
}

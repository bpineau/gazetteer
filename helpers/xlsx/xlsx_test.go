package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// rawWorkbook assembles a workbook from hand-written parts, the way the
// publishers' tools (Excel, LibreOffice Calc) lay one out: shared strings
// and typed cells, which Encode never emits.
func rawWorkbook(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const wbXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Documentation" sheetId="1" state="visible" r:id="rId3"/><sheet name="txcho_ze" sheetId="2" r:id="rId4"/></sheets></workbook>`

const relsXML = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="/xl/worksheets/sheet2.xml"/>
<Relationship Id="rId6" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
</Relationships>`

// Shared strings: plain, rich text (two runs), one with a phonetic run to
// skip, and an entity.
const sstXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="5" uniqueCount="5">
<si><t xml:space="preserve">Taux de chômage localisés par zone d&apos;emploi</t></si>
<si><t>ZE2020</t></si>
<si><r><t>Évry</t></r><r><rPr><b/></rPr><t>-Courcouronnes</t></r></si>
<si><t>東京</t><rPh sb="0" eb="2"><t>とうきょう</t></rPh></si>
<si><t>2026-T2</t></si>
</sst>`

const sheet1XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" s="2" t="s"><v>0</v></c></row>
</sheetData></worksheet>`

// Sheet 2: a banner, two empty rows (one absent, one declared empty), a
// header with a skipped column, then typed cells: shared, numeric (integer,
// decimal, long binary expansion, exponent), boolean, formula string, inline
// string, error, and an empty styled cell.
const sheet2XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" s="12"/><c r="C1" s="13"/></row>
<row r="3" customFormat="false"></row>
<row r="4"><c r="A4" t="s"><v>1</v></c><c r="C4" t="s"><v>4</v></c></row>
<row r="5"><c r="A5" t="s"><v>2</v></c><c r="B5" t="n"><v>34875</v></c><c r="C5"><v>7.4</v></c><c r="D5"><v>7.2999999999999998</v></c><c r="E5"><v>1.5E-3</v></c></row>
<row r="6"><c r="A6" t="s"><v>3</v></c><c r="B6" t="b"><v>1</v></c><c r="C6" t="b"><v>0</v></c><c r="D6" t="str"><f>CONCAT(A6)</f><v>formula text</v></c><c r="E6" t="inlineStr"><is><t>inline </t><t>text</t></is></c><c r="F6" t="e"><v>#N/A</v></c><c r="G6" s="1"/></row>
</sheetData></worksheet>`

func publisherWorkbook(t *testing.T) []byte {
	return rawWorkbook(t, map[string]string{
		"xl/workbook.xml":             wbXML,
		"xl/_rels/workbook.xml.rels":  relsXML,
		"xl/sharedStrings.xml":        sstXML,
		"xl/worksheets/sheet1.xml":    sheet1XML,
		"xl/worksheets/sheet2.xml":    sheet2XML,
		"xl/styles.xml":               `<styleSheet/>`,
		"[Content_Types].xml":         `<Types/>`,
		"_rels/.rels":                 `<Relationships/>`,
		"docProps/app.xml":            `<Properties/>`,
		"xl/printerSettings/p1.bin":   "binary",
		"xl/worksheets/_rels/s1.rels": `<Relationships/>`,
	})
}

func TestRows_PublisherLayout(t *testing.T) {
	t.Parallel()
	wb, err := OpenBytes(publisherWorkbook(t))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if got, want := wb.Sheets(), []string{"Documentation", "txcho_ze"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Sheets = %q, want %q", got, want)
	}
	rows, err := wb.Rows("txcho_ze")
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	want := [][]string{
		{"Taux de chômage localisés par zone d'emploi"},
		{},
		{},
		{"ZE2020", "", "2026-T2"},
		{"Évry-Courcouronnes", "34875", "7.4", "7.3", "0.0015"},
		{"東京", "TRUE", "FALSE", "formula text", "inline text", "#N/A"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("Rows =\n%q\nwant\n%q", rows, want)
	}

	doc, err := wb.Rows("Documentation")
	if err != nil {
		t.Fatalf("Rows(Documentation): %v", err)
	}
	if len(doc) != 1 || doc[0][0] != want[0][0] {
		t.Errorf("Documentation = %q", doc)
	}

	if _, err := wb.Rows("COM"); !errors.Is(err, ErrNoSheet) {
		t.Errorf("Rows(COM) err = %v, want ErrNoSheet", err)
	}
}

func TestRows_WithoutSharedStrings(t *testing.T) {
	t.Parallel()
	b := rawWorkbook(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="S" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Type="w" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c><v>1</v></c><c t="inlineStr"><is><t>x</t></is></c></row><row><c r="B2"><v>2</v></c></row></sheetData></worksheet>`,
	})
	wb, err := OpenBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := wb.Rows("S")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"1", "x"}, {"", "2"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("Rows = %q, want %q", rows, want)
	}
}

func TestRows_Errors(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]string{
		"no workbook part": {
			"xl/_rels/workbook.xml.rels": `<Relationships/>`,
		},
		"dangling relationship": {
			"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="S" r:id="rId9"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships/>`,
		},
		"no sheet": {
			"xl/workbook.xml":            `<workbook><sheets/></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships/>`,
		},
	}
	for name, parts := range cases {
		if _, err := OpenBytes(rawWorkbook(t, parts)); err == nil {
			t.Errorf("%s: Open succeeded, want error", name)
		}
	}
	if _, err := OpenBytes([]byte("not a zip")); err == nil {
		t.Error("garbage: Open succeeded, want error")
	}

	// A shared-string index past the table is a corrupt file, not a blank.
	b := rawWorkbook(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="S" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>a</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>7</v></c></row></sheetData></worksheet>`,
	})
	wb, err := OpenBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wb.Rows("S"); err == nil || !strings.Contains(err.Error(), "shared string index") {
		t.Errorf("out-of-range shared string: err = %v", err)
	}
}

// TestCappedReader: a member whose header understates its size is cut at
// the cap with an error, not inflated to the end.
func TestCappedReader(t *testing.T) {
	t.Parallel()
	r := &cappedReader{ReadCloser: nopCloser{strings.NewReader("0123456789")}, left: 4, part: "p"}
	out, err := readAllErr(r)
	if err == nil || len(out) != 4 || !strings.Contains(err.Error(), "cap") {
		t.Errorf("cappedReader read %d bytes, err %v; want 4 bytes then the cap error", len(out), err)
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }

func readAllErr(r *cappedReader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 3)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, err
		}
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	rows := [][]string{
		{"Table d'appartenance <banner> & co"},
		nil,
		{"CODGEO", "LIBGEO", "", "ZE2020"},
		{"01001", "L'Abergement-Clémenciat", "01", "8405"},
		{"", "", "", "", "trailing", "", ""},
	}
	b, err := Encode(Sheet{Name: "COM", Rows: rows}, Sheet{Name: "Zones_supra_communales", Rows: [][]string{{"NIVGEO"}}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	wb, err := OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if got, want := wb.Sheets(), []string{"COM", "Zones_supra_communales"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Sheets = %q, want %q", got, want)
	}
	got, err := wb.Rows("COM")
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	want := [][]string{
		{"Table d'appartenance <banner> & co"},
		{},
		{"CODGEO", "LIBGEO", "", "ZE2020"},
		{"01001", "L'Abergement-Clémenciat", "01", "8405"},
		{"", "", "", "", "trailing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip =\n%q\nwant\n%q", got, want)
	}
	if _, err := Encode(); err == nil {
		t.Error("Encode() with no sheet succeeded")
	}
	if _, err := Encode(Sheet{Rows: want}); err == nil {
		t.Error("Encode of an unnamed sheet succeeded")
	}
}

func TestColumnNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ref string
		col int
	}{{"A1", 0}, {"Z9", 25}, {"AA1", 26}, {"BW1", 74}, {"ZZ1", 701}, {"AAA1", 702}, {"c3", 2}} {
		got, err := columnOf(tc.ref)
		if err != nil || got != tc.col {
			t.Errorf("columnOf(%q) = %d, %v; want %d", tc.ref, got, err, tc.col)
		}
		if name := columnName(tc.col); !strings.HasPrefix(strings.ToUpper(tc.ref), name) {
			t.Errorf("columnName(%d) = %q, want the letters of %q", tc.col, name, tc.ref)
		}
	}
	if _, err := columnOf("12"); err == nil {
		t.Error("columnOf(\"12\") succeeded")
	}
}

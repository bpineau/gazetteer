package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// Sheet is one worksheet to Encode: its tab name and its cell grid, row
// major, A1 at Rows[0][0]. A nil or empty row leaves the sheet row empty;
// an empty cell is skipped.
type Sheet struct {
	Name string
	Rows [][]string
}

// Encode writes a minimal workbook holding the given sheets, in order.
// Every cell is an inline string, so Rows reads the grid back as given
// (trailing empty cells of a row aside, which Rows drops). The file opens
// in spreadsheet applications, but carries no style, width or format:
// it is for fixtures and plain exports.
func Encode(sheets ...Sheet) ([]byte, error) {
	if len(sheets) == 0 {
		return nil, errors.New("xlsx: Encode needs at least one sheet")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}

	var types, wbSheets, rels strings.Builder
	types.WriteString(xml.Header)
	types.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for i, s := range sheets {
		if strings.TrimSpace(s.Name) == "" {
			return nil, fmt.Errorf("xlsx: sheet %d has no name", i+1)
		}
		part := fmt.Sprintf("worksheets/sheet%d.xml", i+1)
		fmt.Fprintf(&types, `<Override PartName="/xl/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, part)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(s.Name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="%s"/>`, i+1, part)
		if err := put("xl/"+part, sheetXML(s.Rows)); err != nil {
			return nil, err
		}
	}
	types.WriteString(`</Types>`)

	files := []struct{ name, body string }{
		{"[Content_Types].xml", types.String()},
		{"_rels/.rels", xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", xml.Header + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + wbSheets.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
	}
	for _, f := range files {
		if err := put(f.name, f.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sheetXML(rows [][]string) string {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, r := range rows {
		if len(r) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<row r="%d">`, i+1)
		for j, v := range r {
			if v == "" {
				continue
			}
			fmt.Fprintf(&b, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, columnName(j), i+1, esc(v))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

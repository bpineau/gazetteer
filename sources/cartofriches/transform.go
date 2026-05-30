package cartofriches

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input.
const rawName = "cartofriches_friches.raw.csv"

// rawURL is the Cerema "Sites référencés dans Cartofriches" national
// extract (friches-standard.csv) published on data.gouv.fr (dataset slug
// sites-references-dans-cartofriches). data.gouv mints a dated static path
// per revision; bump this when Cerema republishes the export (the slug page
// lists the current CSV resource). One row per referenced friche site.
const rawURL = "https://static.data.gouv.fr/resources/sites-references-dans-cartofriches/20260429-124852/friches-standard-2026-04-15.csv"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr / Cerema — Cartofriches (sites de friches référencés)"

// metaNote describes the aggregation in the rebuilt artifact.
const metaNote = "Aggregate per commune: count of referenced wasteland / brownfield sites, breakdown by type + status, total unite_fonciere surface (m²)."

// CSV column headers (semicolon-separated, quoted, UTF-8). The friches
// export carries one row per referenced site; these are the only columns
// the per-commune aggregate consumes.
const (
	colINSEE   = "comm_insee"             // 5-digit commune code
	colName    = "comm_nom"               // commune label (raw, free-form)
	colType    = "site_type"              // friche type (friche industrielle, …)
	colStatus  = "site_statut"            // friche status (avec/sans projet, …)
	colSurface = "unite_fonciere_surface" // unité foncière surface in m²
)

// naToken is the upstream's literal "missing value" sentinel. The CSV uses
// the R/Cerema convention of an unquoted NA for absent cells; those must
// not be counted as a type/status category nor parsed as a surface.
const naToken = "NA"

// transform rebuilds the processed cartofriches artifact from the upstream
// friches-standard CSV. It groups the one-row-per-site export by commune
// INSEE and, per commune, records: the count of sites (n), a breakdown by
// site_type and by site_statut, the summed unité-foncière surface (m²), and
// the commune label taken from the first row seen for that commune.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(unescapeQuotes(dataset.BOMReader(rc)))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("cartofriches: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	iINSEE, okI := col[colINSEE]
	iName, okN := col[colName]
	iType, okT := col[colType]
	iStatus, okS := col[colStatus]
	iSurface, okF := col[colSurface]
	if !okI || !okN || !okT || !okS || !okF {
		return fmt.Errorf("cartofriches: header missing required columns: %v", header)
	}

	communes := map[string]*Entry{}
	nSites := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("cartofriches: read row: %w", err)
		}
		insee := strings.TrimSpace(field(rec, iINSEE))
		if insee == "" || insee == naToken {
			continue
		}
		nSites++

		e := communes[insee]
		if e == nil {
			e = &Entry{
				Label:    cleanCell(field(rec, iName)),
				ByType:   map[string]int{},
				ByStatus: map[string]int{},
			}
			communes[insee] = e
		}
		e.SiteCount++

		if t := cleanCell(field(rec, iType)); t != "" {
			e.ByType[t]++
		}
		if s := cleanCell(field(rec, iStatus)); s != "" {
			e.ByStatus[s]++
		}
		if m, ok := parseSurface(field(rec, iSurface)); ok {
			e.TotalSurfaceM2 += m
		}
	}

	if nSites == 0 {
		return errors.New("cartofriches: transform produced no sites")
	}

	idx := Index{
		Meta: Meta{
			Source:           metaSource,
			RowCountCommunes: len(communes),
			RowCountSites:    nSites,
			Note:             metaNote,
		},
		Communes: make(map[string]Entry, len(communes)),
	}
	for insee, e := range communes {
		// Drop empty breakdown maps so the artifact omits them
		// (omitempty only fires on nil/zero-length maps).
		if len(e.ByType) == 0 {
			e.ByType = nil
		}
		if len(e.ByStatus) == 0 {
			e.ByStatus = nil
		}
		idx.Communes[insee] = *e
	}

	return json.NewEncoder(dst).Encode(idx)
}

// unescapeQuotes rewrites the upstream's backslash-escaped quotes (`\"`,
// e.g. inside free-text site_nom / commentaire cells like maison de
// retraite \"Les Opalines\") into the RFC-4180 doubled-quote form (`""`)
// so the strict csv.Reader parses them as literal quotes instead of
// misaligning columns. A lone trailing backslash is buffered until the
// next byte is known, so the substitution is correct across reads.
func unescapeQuotes(r io.Reader) io.Reader {
	return &quoteUnescaper{r: r}
}

type quoteUnescaper struct {
	r         io.Reader
	pendingBS bool   // a '\' was held back at the end of the last chunk
	buf       []byte // expanded output not yet returned to the caller
	src       []byte // reusable read buffer
}

func (q *quoteUnescaper) Read(p []byte) (int, error) {
	if len(q.buf) > 0 {
		n := copy(p, q.buf)
		q.buf = q.buf[n:]
		return n, nil
	}
	if cap(q.src) == 0 {
		q.src = make([]byte, 32*1024)
	}
	n, err := q.r.Read(q.src)
	if n == 0 {
		if q.pendingBS && err == io.EOF {
			// Trailing lone backslash at true EOF: emit it verbatim.
			q.pendingBS = false
			if len(p) > 0 {
				p[0] = '\\'
				return 1, err
			}
			q.buf = []byte{'\\'}
			return 0, nil
		}
		return 0, err
	}

	out := make([]byte, 0, n+1)
	in := q.src[:n]
	for i := 0; i < len(in); i++ {
		b := in[i]
		if q.pendingBS {
			q.pendingBS = false
			if b == '"' {
				out = append(out, '"', '"')
				continue
			}
			out = append(out, '\\')
		}
		if b == '\\' {
			if i == len(in)-1 {
				q.pendingBS = true
				break
			}
			if in[i+1] == '"' {
				out = append(out, '"', '"')
				i++
				continue
			}
			out = append(out, '\\')
			continue
		}
		out = append(out, b)
	}

	n2 := copy(p, out)
	if n2 < len(out) {
		q.buf = append(q.buf[:0], out[n2:]...)
	}
	// Suppress EOF if we still owe buffered bytes; surface it next call.
	if err == io.EOF && (len(q.buf) > 0 || q.pendingBS) {
		err = nil
	}
	return n2, err
}

// validate gates publication: the rebuilt artifact must parse, be
// non-empty, and carry a positive site count.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("cartofriches: validated artifact has no communes")
	}
	if idx.Meta.RowCountSites <= 0 {
		return errors.New("cartofriches: validated artifact has no sites")
	}
	return nil
}

// field returns rec[i] or "" when i is out of range (the export carries a
// fixed schema, but FieldsPerRecord is relaxed for resilience).
func field(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}

// cleanCell trims a cell and maps the upstream NA sentinel to "".
func cleanCell(s string) string {
	s = strings.TrimSpace(s)
	if s == naToken {
		return ""
	}
	return s
}

// parseSurface parses an unité-foncière surface (m², integer). It returns
// (0,false) for blank/NA/non-numeric cells so they don't inflate the sum.
func parseSurface(s string) (int, bool) {
	s = cleanCell(s)
	if s == "" {
		return 0, false
	}
	// The upstream surface is an integer m² count; tolerate a stray
	// decimal by truncating toward zero.
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

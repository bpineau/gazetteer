package sitadel

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Datadir filenames of the two upstream raw inputs: the data CSV and the
// DIDO metadata of the same datafile.
const (
	rawName     = "sitadel.raw.csv"
	rawMetaName = "sitadel.raw.meta.json"
)

// didoDatafile is the DIDO API endpoint of the SDES Sitadel annual communal
// datafile ("Données annuelles communales - Logements").
const didoDatafile = "https://data.statistiques.developpement-durable.gouv.fr/dido/api/v1/datafiles/9c90a880-4ba0-49b4-b99d-d7dd6c810dd0"

// rawURL is that datafile as a semicolon CSV (UTF-8, header row), and
// rawMetaURL its DIDO metadata. Neither names a millésime, so both serve the
// latest one: DIDO stops serving a superseded millésime (a pinned
// ?millesime=2026-06 answered HTTP 404 once 2026-09 was out), which made any
// pin a quarterly break. The metadata records which millésime that is
// (didoMeta), and its row count lets transform check that the two downloads
// saw the same one.
const (
	rawURL     = didoDatafile + "/csv?withColumnName=true"
	rawMetaURL = didoDatafile
)

// didoMeta is the part of the DIDO datafile metadata the transform reads.
type didoMeta struct {
	Millesime string `json:"millesime"` // e.g. "2026-09"
	Rows      int    `json:"rows"`      // data rows of that millésime's CSV
}

// millesimeRE is the shape of a DIDO millésime ("YYYY-MM").
var millesimeRE = regexp.MustCompile(`^\d{4}-\d{2}$`)

// readDidoMeta decodes and checks the datafile metadata raw input.
func readDidoMeta(raw dataset.RawSet) (didoMeta, error) {
	rc, err := raw.Open(rawMetaName)
	if err != nil {
		return didoMeta{}, err
	}
	defer func() { _ = rc.Close() }()
	var m didoMeta
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return didoMeta{}, fmt.Errorf("sitadel: decode DIDO metadata: %w", err)
	}
	if !millesimeRE.MatchString(m.Millesime) || m.Rows <= 0 {
		return didoMeta{}, fmt.Errorf("sitadel: DIDO metadata without a millésime or a row count: %+v", m)
	}
	return m, nil
}

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "SDES Sitadel — logements autorisés et commencés par commune (DIDO datafile 9c90a880-4ba0-49b4-b99d-d7dd6c810dd0)"

// metaNote documents the derivation, recorded into the rebuilt artifact.
const metaNote = "Per-commune annual building permits (LOG_AUT, authorised) and " +
	"housing starts (LOG_COM, commencés), dwellings count. Blank cells are kept " +
	"distinct from a real 0 (stored as -1). Paris/Lyon arrondissement rows are " +
	"folded onto their parent commune (75056/69123); the upstream already " +
	"publishes the parent aggregate. SDP_* floor-area columns are ignored."

// Upstream column headers (SDES Sitadel DIDO CSV).
const (
	colYear  = "ANNEE"
	colINSEE = "COMM"
	colType  = "TYPE_LGT"
	colAuth  = "LOG_AUT"
	colCom   = "LOG_COM"
)

// TYPE_LGT values we read.
const (
	typeTous      = "Tous Logements"
	typeCollectif = "Collectif"
)

// acc accumulates one commune's per-year values during the build, keyed by
// year. Each value pointer is nil for an absent/blank cell.
type acc struct {
	auth     map[int]*int // Tous Logements LOG_AUT
	started  map[int]*int // Tous Logements LOG_COM
	collAuth map[int]*int // Collectif LOG_AUT
}

func newAcc() *acc {
	return &acc{
		auth:     map[int]*int{},
		started:  map[int]*int{},
		collAuth: map[int]*int{},
	}
}

// transform rebuilds the processed sitadel artifact from the upstream DIDO
// CSV. It collects per-commune per-year "Tous Logements" LOG_AUT / LOG_COM and
// "Collectif" LOG_AUT, folding Paris/Lyon arrondissement codes onto their
// parent commune, drops communes with no non-zero authorised data, and emits
// the compact gzipped JSON index.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	meta, err := readDidoMeta(raw)
	if err != nil {
		return err
	}
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("sitadel: read header: %w", err)
	}
	yearC := indexOf(header, colYear)
	inseeC := indexOf(header, colINSEE)
	typeC := indexOf(header, colType)
	authC := indexOf(header, colAuth)
	comC := indexOf(header, colCom)
	if yearC < 0 || inseeC < 0 || typeC < 0 || authC < 0 || comC < 0 {
		return fmt.Errorf("sitadel: header missing required columns: %v", header)
	}
	maxC := maxInt(yearC, inseeC, typeC, authC, comC)

	byCommune := map[string]*acc{}
	rows := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("sitadel: read row: %w", err)
		}
		rows++
		if len(rec) <= maxC {
			continue
		}
		code := strings.TrimSpace(rec[inseeC])
		if len(code) != 5 {
			continue // national/department aggregates or blanks
		}
		// Fold Paris/Lyon arrondissements onto the parent commune. The
		// upstream already publishes the parent aggregate row, so an
		// arrondissement-coded row (folded != raw) is redundant: skip it.
		if communes.FoldArrondissement(code) != code {
			continue
		}
		year, ok := parseYear(rec[yearC])
		if !ok {
			continue
		}
		typ := strings.TrimSpace(rec[typeC])

		a := byCommune[code]
		switch typ {
		case typeTous:
			if a == nil {
				a = newAcc()
				byCommune[code] = a
			}
			a.auth[year] = parseCell(rec[authC])
			a.started[year] = parseCell(rec[comC])
		case typeCollectif:
			if a == nil {
				a = newAcc()
				byCommune[code] = a
			}
			a.collAuth[year] = parseCell(rec[authC])
		}
	}

	// The CSV must be the millésime the metadata names: a new one published
	// between the two downloads would otherwise be labelled with the old name.
	if rows != meta.Rows {
		return fmt.Errorf("sitadel: CSV has %d rows, DIDO metadata of millésime %s says %d: the millésime changed mid-download, refresh again", rows, meta.Millesime, meta.Rows)
	}

	idx := Index{
		Meta: Meta{
			Source:        metaSource,
			DataMillesime: meta.Millesime,
			Note:          metaNote,
		},
		Communes: map[string]Entry{},
	}
	for code, a := range byCommune {
		e, ok := buildEntry(a)
		if !ok {
			continue // no non-zero authorised data
		}
		idx.Communes[code] = e
	}
	if len(idx.Communes) == 0 {
		return errors.New("sitadel: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("sitadel: encode json: %w", err)
	}
	return nil
}

// buildEntry projects an accumulator into a contiguous-year Entry. The year
// span runs from the earliest to the latest year seen across any measure; a
// year with no "Tous Logements" authorised cell stores `missing`. ok is false
// when the commune carries no non-zero authorised dwelling in any year.
func buildEntry(a *acc) (Entry, bool) {
	years := yearSpan(a)
	if len(years) == 0 {
		return Entry{}, false
	}
	y0 := years[0]
	yN := years[len(years)-1]
	n := yN - y0 + 1

	e := Entry{
		YearStart: y0,
		Auth:      make([]int, n),
		Started:   make([]int, n),
		CollAuth:  make([]int, n),
	}
	anyAuth := false
	for i := 0; i < n; i++ {
		y := y0 + i
		e.Auth[i] = cellOr(a.auth[y], missing)
		e.Started[i] = cellOr(a.started[y], missing)
		e.CollAuth[i] = cellOr(a.collAuth[y], missing)
		if e.Auth[i] > 0 {
			anyAuth = true
		}
	}
	if !anyAuth {
		return Entry{}, false
	}
	return e, true
}

// yearSpan returns the sorted distinct years present across all measures.
func yearSpan(a *acc) []int {
	seen := map[int]struct{}{}
	for y := range a.auth {
		seen[y] = struct{}{}
	}
	for y := range a.started {
		seen[y] = struct{}{}
	}
	for y := range a.collAuth {
		seen[y] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for y := range seen {
		out = append(out, y)
	}
	sort.Ints(out)
	return out
}

// validate gates publication: the rebuilt (gzipped) artifact must gunzip,
// parse and be non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("sitadel: validated artifact has no communes")
	}
	return nil
}

// indexOf returns the index of the column whose trimmed header equals name,
// or -1.
func indexOf(header []string, name string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == name {
			return i
		}
	}
	return -1
}

// parseYear parses the ANNEE cell.
func parseYear(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	y, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return y, true
}

// parseCell parses a numeric count cell. A blank cell returns nil (no data,
// kept distinct from a real 0).
func parseCell(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &v
}

// cellOr dereferences p or returns def when nil.
func cellOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func maxInt(xs ...int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

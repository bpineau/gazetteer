package catnat

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// recentWindowYears is the width of the "recent" decree window, measured back
// from the dataset's latest event year.
const recentWindowYears = 10

// Raw input (datadir basename) and upstream URL. Géorisques publishes the GASPAR
// base as a single ZIP bundling several CSVs; catnat_gaspar.csv holds one row per
// (commune, recognised event) since 1982.
const (
	rawName = "catnat_gaspar.zip"
	rawURL  = "https://files.georisques.fr/GASPAR/gaspar.zip"
)

// transform rebuilds the gzipped per-commune aggregate from the GASPAR export.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	buf, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("catnat: read archive: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return fmt.Errorf("catnat: open zip: %w", err)
	}
	csvBytes, err := zipMember(zr, "catnat")
	if err != nil {
		return fmt.Errorf("catnat: member: %w", err)
	}

	p, err := aggregate(csvBytes)
	if err != nil {
		return err
	}
	if len(p.Communes) == 0 {
		return errors.New("catnat: transform produced no communes")
	}

	return dataset.WriteGzJSON(dst, p)
}

// agg accumulates one commune's decrees during the single CSV pass.
type agg struct {
	total, inond, sech, mvt, temp, lastYear int
	years                                   []int
}

// GASPAR column names, each with the spellings the export has used: the
// 2026 export renamed cod_nat_catnat, cod_commune and dat_deb to id_gaspar,
// code_commune and date_debut (and its dates gained a time of day, which
// yearOf ignores). Matching is case-insensitive.
var (
	colDecree  = []string{"id_gaspar", "cod_nat_catnat"} // decree id (NOR)
	colCommune = []string{"code_commune", "cod_commune"} // commune INSEE
	colRisk    = []string{"lib_risque_jo"}               // peril label (JO)
	colStart   = []string{"date_debut", "dat_deb"}       // event start date
)

// column returns the index of the first of names present in col.
func column(col map[string]int, names []string) (int, bool) {
	for _, n := range names {
		if i, ok := col[n]; ok {
			return i, true
		}
	}
	return 0, false
}

// aggregate parses the catnat CSV and folds it into per-commune rows. The recent
// window is measured against the latest event year in the data, so the output is
// deterministic (independent of when the transform runs).
//
// A row is one (decree, commune, peril, event) recognition. The export has
// carried exact duplicates of such rows (some 12 600 in 2025), which would
// count one recognition twice, so a repeated (decree, commune, peril, start
// date) is skipped. Two distinct decrees for the same event (a rectifying
// decree) both count, as in the official register.
func aggregate(csvBytes []byte) (processed, error) {
	r := csv.NewReader(dataset.BOMReader(bytes.NewReader(csvBytes)))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return processed{}, fmt.Errorf("catnat: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ciDecree, ok0 := column(col, colDecree)
	ciCom, ok1 := column(col, colCommune)
	ciRisk, ok2 := column(col, colRisk)
	ciDeb, ok3 := column(col, colStart)
	if !ok0 || !ok1 || !ok2 || !ok3 {
		return processed{}, fmt.Errorf("catnat: missing columns (have %v)", header)
	}

	byCom := map[string]*agg{}
	seen := map[[4]string]bool{}
	maxYear := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return processed{}, fmt.Errorf("catnat: read row: %w", err)
		}
		insee := strings.TrimSpace(at(rec, ciCom))
		if insee == "" {
			continue
		}
		start := strings.TrimSpace(at(rec, ciDeb))
		key := [4]string{strings.TrimSpace(at(rec, ciDecree)), insee, strings.TrimSpace(at(rec, ciRisk)), dateOf(start)}
		if seen[key] {
			continue
		}
		seen[key] = true
		year := yearOf(start)
		a := byCom[insee]
		if a == nil {
			a = &agg{}
			byCom[insee] = a
		}
		a.total++
		switch categorize(at(rec, ciRisk)) {
		case "inond":
			a.inond++
		case "sech":
			a.sech++
		case "mvt":
			a.mvt++
		case "temp":
			a.temp++
		}
		if year > 0 {
			a.years = append(a.years, year)
			if year > a.lastYear {
				a.lastYear = year
			}
			if year > maxYear {
				maxYear = year
			}
		}
	}

	threshold := maxYear - recentWindowYears + 1
	communes := make([]Entry, 0, len(byCom))
	for insee, a := range byCom {
		recent := 0
		for _, y := range a.years {
			if y >= threshold {
				recent++
			}
		}
		communes = append(communes, Entry{
			INSEE: insee, Total: a.total, Recent: recent, LastYear: a.lastYear,
			Inond: a.inond, Sech: a.sech, Mvt: a.mvt, Temp: a.temp,
		})
	}
	// Deterministic order for byte-stable output.
	sort.Slice(communes, func(i, j int) bool { return communes[i].INSEE < communes[j].INSEE })

	return processed{RefYear: maxYear, WindowYears: recentWindowYears, Communes: communes}, nil
}

// categorize folds a JO risk label into one of the four investor-relevant
// inland buckets. Everything else — coastal submersion ("Chocs Mécaniques liés
// à l'action des Vagues", "Raz de Marée"), séisme, grêle, poids de la neige,
// avalanche — deliberately falls through to "" and is counted only in the total:
// the four buckets are the signals that matter for a typical (inland) purchase,
// while TotalArretes still reflects the commune's full history.
func categorize(lib string) string {
	switch {
	case strings.HasPrefix(lib, "Inondation"):
		return "inond"
	case strings.HasPrefix(lib, "Sécheresse"):
		return "sech"
	case strings.Contains(lib, "Mouvement de Terrain"),
		strings.Contains(lib, "Glissement"),
		strings.Contains(lib, "Eboulement"),
		strings.Contains(lib, "Effondrement"):
		return "mvt"
	case strings.HasPrefix(lib, "Tempête"), strings.Contains(lib, "Vents Cycloniques"):
		return "temp"
	default:
		return ""
	}
}

// yearOf extracts the 4-digit year from an ISO date ("1985-01-01" → 1985).
// Years outside a plausible CatNat range (the régime started in 1982) are
// rejected as 0, so a single typo'd date can't push the reference year — and
// thus the recent window for every commune — off into the future.
func yearOf(isoDate string) int {
	s := strings.TrimSpace(isoDate)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y < 1982 || y > 2100 {
		return 0
	}
	return y
}

// dateOf returns the calendar-date part of a GASPAR timestamp
// ("1999-12-25 12:00:00" → "1999-12-25"); a plain date is returned as is.
// The time of day the 2026 export appends varies between rows of one decree
// (00:00, 01:00, 02:00, 12:00) and carries no meaning.
func dateOf(s string) string {
	if d, _, ok := strings.Cut(s, " "); ok {
		return d
	}
	return s
}

// at returns the i-th field of rec, or "" when out of range.
func at(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}

// zipMember returns the bytes of the UNIQUE .csv member whose base name
// contains nameSubstr (skipping macOS resource forks). It errors on zero or more
// than one match, so a future archive-layout change fails loudly rather than
// silently aggregating the wrong file.
func zipMember(zr *zip.Reader, nameSubstr string) ([]byte, error) {
	var match *zip.File
	for _, f := range zr.File {
		base := strings.ToLower(path.Base(f.Name))
		if strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(base, "._") {
			continue
		}
		if !strings.HasSuffix(base, ".csv") || !strings.Contains(base, nameSubstr) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("ambiguous: multiple .csv members match %q (%s, %s)", nameSubstr, match.Name, f.Name)
		}
		match = f
	}
	if match == nil {
		return nil, fmt.Errorf("no .csv member matching %q", nameSubstr)
	}
	rc, err := match.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// validate gates a freshly-built artifact: it must gunzip, parse, and carry a
// plausible number of communes with a sane reference year.
func validate(r io.Reader) error {
	p, err := dataset.ReadGzJSON[processed](r, Name)
	if err != nil {
		return err
	}
	if len(p.Communes) < 30000 {
		return fmt.Errorf("catnat: only %d communes, want ≥ 30000", len(p.Communes))
	}
	if p.RefYear < 2000 || p.RefYear > 2100 {
		return fmt.Errorf("catnat: implausible ref year %d", p.RefYear)
	}
	return nil
}

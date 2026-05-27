package pinel

import (
	"bytes"
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:embed data/zonage_abc_communes.csv
var pinelFS embed.FS

// Entry captures the ABC zoning row for one commune.
type Entry struct {
	// INSEE is the 5-digit commune code (zero-padded).
	INSEE string
	// CommuneLabel is the human-readable commune name as it appears in
	// the source file.
	CommuneLabel string
	// Zone is the ABC classification.
	Zone Zone
}

// Index is the per-INSEE lookup index.
type Index struct {
	byInsee map[string]Entry
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, parsing the embedded CSV on first
// call.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := pinelFS.ReadFile("data/zonage_abc_communes.csv")
		if err != nil {
			indexErr = fmt.Errorf("pinel: read zonage_abc_communes: %w", err)
			return
		}
		idx, err := parseCSV(raw)
		if err != nil {
			indexErr = fmt.Errorf("pinel: parse zonage_abc_communes: %w", err)
			return
		}
		indexCache = idx
	})
	return indexCache, indexErr
}

// Lookup returns the zoning entry for the given INSEE. `ok` is false
// when the commune is absent from the dataset.
//
// The upstream file stores only the parent commune for Paris (75056),
// Lyon (69123) and Marseille (13055); arrondissement codes
// (75101…75120, 69381…69389, 13201…13216) are folded to the parent so
// the lookup succeeds for either form.
func (idx *Index) Lookup(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Entry{}, false
	}
	if e, ok := idx.byInsee[insee]; ok {
		return e, true
	}
	if parent, ok := foldArrondissement(insee); ok {
		if e, ok := idx.byInsee[parent]; ok {
			return e, true
		}
	}
	return Entry{}, false
}

// foldArrondissement maps the INSEE code of a Paris / Lyon / Marseille
// arrondissement to the parent commune's INSEE. Returns ("", false)
// for everything else.
func foldArrondissement(insee string) (string, bool) {
	if len(insee) != 5 {
		return "", false
	}
	switch insee[:3] {
	case "751":
		return "75056", true
	case "693":
		// 69381..69389 only; other 69-codes are independent communes.
		if insee[3] == '8' && insee[4] >= '1' && insee[4] <= '9' {
			return "69123", true
		}
	case "132":
		// 13201..13216 only.
		if insee[3] == '0' && insee[4] >= '1' && insee[4] <= '9' {
			return "13055", true
		}
		if insee[3] == '1' && insee[4] >= '0' && insee[4] <= '6' {
			return "13055", true
		}
	}
	return "", false
}

// Count returns the number of communes in the dataset.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.byInsee)
}

// parseCSV reads the upstream `id,commune,zonage` schema. The id
// column is the 5-digit INSEE (already zero-padded in the upstream
// file). Quotes are kept for INSEE codes that begin with a leading
// zero (the encoding-friendly habit at data.gouv.fr).
func parseCSV(raw []byte) (*Index, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = ','
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, name := range header {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}
	required := []string{"id", "commune", "zonage"}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("missing column %q in header %v", name, header)
		}
	}
	out := make(map[string]Entry, 36_000)
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		insee := strings.TrimSpace(rec[col["id"]])
		if insee == "" {
			continue
		}
		zone := normaliseZone(rec[col["zonage"]])
		if zone == ZoneUnknown {
			continue
		}
		out[insee] = Entry{
			INSEE:        insee,
			CommuneLabel: strings.TrimSpace(rec[col["commune"]]),
			Zone:         zone,
		}
	}
	return &Index{byInsee: out}, nil
}

// normaliseZone normalises the upstream code (which sometimes carries
// stray whitespace) into a Zone enum value. Unknown labels collapse
// to ZoneUnknown so the loader silently skips them rather than panic.
func normaliseZone(s string) Zone {
	switch strings.TrimSpace(s) {
	case "A bis", "Abis", "ABIS", "A BIS":
		return ZoneAbis
	case "A":
		return ZoneA
	case "B1":
		return ZoneB1
	case "B2":
		return ZoneB2
	case "C":
		return ZoneC
	default:
		return ZoneUnknown
	}
}

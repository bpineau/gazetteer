package carteloyers

import (
	"compress/gzip"
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/frnorm"
)

//go:embed data/carte_loyers_appartement.csv.gz data/carte_loyers_maison.csv.gz data/carte_loyers_app12.csv.gz data/carte_loyers_app3.csv.gz
var embedFS embed.FS

// typologyFile binds one INRAE typology to its embedded CSV. Each file is
// an independent dataset.Set so the datadir override and refresh tooling
// operate per typology. Read-only until each Transform is reconstructed.
type typologyFile struct {
	typ Typology
	set dataset.Set
}

// newSet builds the refreshable Set for one typology: its embedded processed
// CSV, the upstream raw it is rebuilt from, and the shared transform.
func newSet(processed, rawName, rawURL string) dataset.Set {
	return dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: processed},
		Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
		Transform: makeTransform(rawName),
		Validate:  validate,
	}
}

var fileSets = []typologyFile{
	{TypologyApartment, newSet("carte_loyers_appartement.csv.gz", "carte_loyers.raw.appartement.csv", urlAppartement)},
	{TypologyHouse, newSet("carte_loyers_maison.csv.gz", "carte_loyers.raw.maison.csv", urlMaison)},
	{TypologyApt12, newSet("carte_loyers_app12.csv.gz", "carte_loyers.raw.app12.csv", urlApt12)},
	{TypologyApt3, newSet("carte_loyers_app3.csv.gz", "carte_loyers.raw.app3.csv", urlApt3)},
}

// Row captures one INSEE × typology observation. Loyers are in
// EUR/m²/month, charges comprises (CC).
type Row struct {
	InseeCode    string
	Department   string
	LoyerMedCC   float64 // loypredm2 — médiane EUR/m²/mois CC
	LoyerLowerCC float64 // lwr_IPm2 — borne basse intervalle de prédiction
	LoyerUpperCC float64 // upr_IPm2 — borne haute intervalle de prédiction
	PredType     string  // "maille" (rare obs, mailled neighbours) | "commune" (≥ floor)
	NbObsCommune int     // nombre d'observations sur la commune
}

// Index holds the lookup index for every typology.
type Index struct {
	byTypology map[Typology]map[string]Row
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton lookup index, resolving each typology CSV
// from dir (the datadir) with a fallback to the embedded copies and
// parsing them on first call. The dir from the first call wins for the
// process lifetime. A missing (non-embedded) typology contributes an empty
// table rather than failing the whole index.
//
// Errors are sticky: if the first call fails, every subsequent call returns
// the same error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		indexCache, indexErr = parseAll(dir)
	})
	return indexCache, indexErr
}

// Lookup returns the carte des loyers observation for the given INSEE
// code under the requested typology. The `ok` flag is false when the
// INSEE is not in the dataset (e.g. DOM-TOM, recently-merged commune).
func (idx *Index) Lookup(insee string, typ Typology) (Row, bool) {
	if idx == nil {
		return Row{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Row{}, false
	}
	per, ok := idx.byTypology[typ]
	if !ok {
		return Row{}, false
	}
	r, ok := per[insee]
	return r, ok
}

// Count returns the number of observations parsed for the given
// typology. Useful for tests and operator-facing tools.
func (idx *Index) Count(typ Typology) int {
	if idx == nil {
		return 0
	}
	return len(idx.byTypology[typ])
}

func parseAll(dir string) (*Index, error) {
	idx := &Index{
		byTypology: map[Typology]map[string]Row{},
	}
	for _, f := range fileSets {
		rows, err := loadTypology(f.set, dir)
		if err != nil {
			return nil, fmt.Errorf("carteloyers: %s: %w", f.set.Processed.Name, err)
		}
		idx.byTypology[f.typ] = rows
	}
	return idx, nil
}

// loadTypology resolves and parses one typology CSV. A missing
// (non-embedded) file yields an empty table rather than an error.
func loadTypology(s dataset.Set, dir string) (map[string]Row, error) {
	rc, err := s.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return map[string]Row{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	zr, err := gzip.NewReader(rc)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	return parseCSV(zr)
}

func parseCSV(src io.Reader) (map[string]Row, error) {
	r := csv.NewReader(src)
	r.Comma = ';'
	// header
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimSpace(name)] = i
	}
	required := []string{"INSEE_C", "DEP", "loypredm2", "lwr_IPm2", "upr_IPm2", "TYPPRED", "nbobs_com"}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("missing column %q in header %v", name, header)
		}
	}
	out := make(map[string]Row, 35_000)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		insee := strings.TrimSpace(rec[col["INSEE_C"]])
		if insee == "" {
			continue
		}
		med, ok1 := frnorm.ParseFRFloat(rec[col["loypredm2"]])
		lo, ok2 := frnorm.ParseFRFloat(rec[col["lwr_IPm2"]])
		hi, ok3 := frnorm.ParseFRFloat(rec[col["upr_IPm2"]])
		if !ok1 || !ok2 || !ok3 {
			// Skip malformed rows quietly — the dataset rarely
			// carries them but we don't want a single bad value
			// to abort startup.
			continue
		}
		nb, _ := strconv.Atoi(strings.TrimSpace(rec[col["nbobs_com"]]))
		out[insee] = Row{
			InseeCode:    insee,
			Department:   strings.TrimSpace(rec[col["DEP"]]),
			LoyerMedCC:   med,
			LoyerLowerCC: lo,
			LoyerUpperCC: hi,
			PredType:     strings.TrimSpace(rec[col["TYPPRED"]]),
			NbObsCommune: nb,
		}
	}
	return out, nil
}

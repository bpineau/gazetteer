package dvfagg

import (
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/dvf_communes.csv
var embedFS embed.FS

const embedName = "dvf_communes.csv"

// theSet is the single refreshable dataset this source owns.
func newSet() dataset.Set {
	return dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: embedName},
		Raw:       rawFiles(),
		Transform: transform,
		Validate:  validate,
	}
}

var theSet = newSet()

// Index is the in-memory INSEE → aggregate lookup.
type Index struct{ byINSEE map[string]Result }

// Lookup returns the commune aggregate, or (zero,false).
func (idx *Index) Lookup(insee string) (Result, bool) {
	if idx == nil {
		return Result{}, false
	}
	r, ok := idx.byINSEE[strings.TrimSpace(insee)]
	return r, ok
}

// Codes returns the sorted list of INSEE codes that have price data.
func (idx *Index) Codes() []string {
	if idx == nil {
		return nil
	}
	out := make([]string, 0, len(idx.byINSEE))
	for k := range idx.byINSEE {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Count returns the number of communes parsed (tests/tools).
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.byINSEE)
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the CSV from dir (datadir)
// with embedded fallback. Sticky: first call's dir and error win.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(theSet, dir, parseCSV)
}

func parseCSV(src io.Reader) (*Index, error) {
	r := csv.NewReader(src)
	r.Comma = ';'
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("dvfagg: header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, n := range []string{"INSEE_C", "DEP", "n", "p25", "p50", "p75", "n_small", "p50_small"} {
		if _, ok := col[n]; !ok {
			return nil, fmt.Errorf("dvfagg: missing column %q", n)
		}
	}
	out := &Index{byINSEE: make(map[string]Result, 35_000)}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("dvfagg: row: %w", err)
		}
		ins := strings.TrimSpace(rec[col["INSEE_C"]])
		if ins == "" {
			continue
		}
		pf := func(name string) float64 {
			v, _ := strconv.ParseFloat(strings.TrimSpace(rec[col[name]]), 64)
			return v
		}
		pi := func(name string) int {
			v, _ := strconv.Atoi(strings.TrimSpace(rec[col[name]]))
			return v
		}
		out.byINSEE[ins] = Result{
			Dept:                  strings.TrimSpace(rec[col["DEP"]]),
			N:                     pi("n"),
			NSmall:                pi("n_small"),
			PriceP25EURM2:         pf("p25"),
			PriceMedianEURM2:      pf("p50"),
			PriceP75EURM2:         pf("p75"),
			PriceMedianSmallEURM2: pf("p50_small"),
		}
	}
	return out, nil
}

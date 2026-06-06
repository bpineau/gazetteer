package delinquance

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/delinquance_communes.json.gz
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Refresh
// downloads the upstream SSMSI commune CSV and rebuilds the gzipped JSON
// index via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "delinquance_communes.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry is one commune's row from the SSMSI dataset.
type Entry struct {
	// Population is the INSEE-published resident population the SSMSI
	// uses as the rate denominator.
	Population int `json:"pop"`
	// Rates maps indicator handles to per-thousand rates (events per
	// 1 000 inhabitants, or per 1 000 logements for burglary).
	Rates map[string]float64 `json:"ind"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string   `json:"source"`
	DataYear         int      `json:"data_year"`
	Unit             string   `json:"unit"`
	RowCountCommunes int      `json:"row_count_communes"`
	Indicators       []string `json:"indicators"`
	Note             string   `json:"note"`
}

// Index is the per-INSEE lookup index.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, and parsing it on
// first call. Subsequent calls are constant-time and ignore dir — the dir
// from the first call wins for the process lifetime. A dataset that is
// neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			indexCache = &Index{}
			return
		}
		if err != nil {
			indexErr = fmt.Errorf("delinquance: open dataset: %w", err)
			return
		}
		defer func() { _ = rc.Close() }()
		idx, err := parseIndex(rc)
		if err != nil {
			indexErr = err
			return
		}
		indexCache = idx
	})
	return indexCache, indexErr
}

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("delinquance: gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("delinquance: read gunzipped body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("delinquance: parse json: %w", err)
	}
	return &idx, nil
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune is absent (rare — typically the smallest communes with
// every indicator masked by the secret-statistique rule).
func (idx *Index) Lookup(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Entry{}, false
	}
	e, ok := idx.Communes[insee]
	return e, ok
}

// Level returns the social-distress RiskFlag for the given INSEE. It
// returns RiskUnknown when the commune is absent from the dataset or when
// per-inhabitant rates are inflated (arrondissement-split cities such as
// Paris/Lyon/Marseille — see classifyRisk documentation).
func (idx *Index) Level(insee string) RiskFlag {
	if idx == nil {
		return RiskUnknown
	}
	if hasInflatedPerInhabitantRates(insee) {
		return RiskUnknown
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return RiskUnknown
	}
	return classifyRisk(e.Rates)
}

// Count returns the number of communes with at least one indicator
// populated.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

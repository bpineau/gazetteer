package vacance_logements

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

//go:embed data/vacance_logements_communes.json.gz
var embedFS embed.FS

// set binds the embedded INSEE base-logement extract to the datadir/refresh
// pipeline. Refresh downloads the upstream INSEE zip and rebuilds the
// gzipped JSON via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "vacance_logements_communes.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry is one commune's row from the INSEE base logement census.
type Entry struct {
	// Log is P21_LOG — total logements.
	Log int `json:"log"`
	// Vac is P21_LOGVAC — vacant logements.
	Vac int `json:"vac"`
	// RP is P21_RP — résidences principales.
	RP int `json:"rp"`
	// RSec is P21_RSECOCC — résidences secondaires + logements occasionnels.
	RSec int `json:"rsec"`
	// VacancyRatePct is the pre-computed VAC/LOG ratio (percent),
	// rounded to two decimals.
	VacancyRatePct float64 `json:"vacancy_rate_pct"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	DataYear         int    `json:"data_year"`
	RowCountCommunes int    `json:"row_count_communes"`
	Note             string `json:"note,omitempty"`
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
			indexErr = fmt.Errorf("vacance_logements: open dataset: %w", err)
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
		return nil, fmt.Errorf("vacance_logements: gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("vacance_logements: read gunzipped body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("vacance_logements: parse json: %w", err)
	}
	return &idx, nil
}

// Lookup returns the entry for the given INSEE. `ok` is false when the
// commune is absent (rare — a handful of communes dropped from the
// census between vintages).
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

// Count returns the number of communes in the loaded extract.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

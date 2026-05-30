package zonageabc

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/zonage_abc_communes.json
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Refresh
// downloads the upstream CSV and rebuilds the indexed JSON via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "zonage_abc_communes.json"},
	Raw:       []dataset.File{{Name: rawCSVName, URL: rawCSVURL}},
	Transform: transform,
	Validate:  validate,
}

// Meta carries the manifest metadata for the embedded dataset.
type Meta struct {
	Source           string `json:"source"`
	DownloadedAt     string `json:"downloaded_at"`
	EffectiveDate    string `json:"effective_date"`
	RowCountCommunes int    `json:"row_count_communes"`
	Note             string `json:"note"`
}

// Index carries the per-commune zonage classification.
type Index struct {
	Meta     Meta            `json:"meta"`
	Communes map[string]Zone `json:"communes"`
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
			indexErr = fmt.Errorf("zonageabc: open dataset: %w", err)
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

// parseIndex decodes the JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("zonageabc: read dataset: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("zonageabc: parse dataset: %w", err)
	}
	return &idx, nil
}

// Lookup returns the zonage classification for the given INSEE. `ok`
// is false when the commune is absent (rare; only fires for INSEE
// codes that do not exist in the September 2025 revision).
func (idx *Index) Lookup(insee string) (Zone, bool) {
	if idx == nil {
		return ZoneUnknown, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return ZoneUnknown, false
	}
	z, ok := idx.Communes[insee]
	if !ok {
		return ZoneUnknown, false
	}
	return z, true
}

// Count returns the number of communes in the dataset.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

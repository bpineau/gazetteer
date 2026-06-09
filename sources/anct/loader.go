package anct

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/anct_programmes.json
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Refresh
// downloads the three upstream programme lists (ACV, PVD, ORT) and rebuilds
// the merged per-commune JSON via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "anct_programmes.json"},
	Raw: []dataset.File{
		{Name: rawACVName, URL: rawACVURL},
		{Name: rawPVDName, URL: rawPVDURL},
		{Name: rawORTName, URL: rawORTURL},
	},
	Transform: transform,
	Validate:  validate,
}

// Entry is one commune's row from the merged ACV / PVD / ORT extract.
type Entry struct {
	Label string `json:"label,omitempty"`

	ACV         bool   `json:"acv,omitempty"`
	ACVSignedAt string `json:"acv_signed_at,omitempty"`

	PVD         bool   `json:"pvd,omitempty"`
	PVDSignedAt string `json:"pvd_signed_at,omitempty"`

	ORT         bool   `json:"ort,omitempty"`
	ORTSignedAt string `json:"ort_signed_at,omitempty"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountACV      int    `json:"row_count_acv"`
	RowCountPVD      int    `json:"row_count_pvd"`
	RowCountORT      int    `json:"row_count_ort"`
	Note             string `json:"note"`
}

// Index is the per-INSEE lookup index.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, and parsing it on
// first call. Subsequent calls are constant-time and ignore dir — the dir
// from the first call wins for the process lifetime. A dataset that is
// neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("anct: read body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("anct: parse json: %w", err)
	}
	return &idx, nil
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune participates in none of the three programmes.
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

// Count returns the number of communes flagged for at least one
// programme.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

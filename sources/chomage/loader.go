package chomage

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/chomage_zones_emploi.json
var embedFS embed.FS

// set binds the embedded chômage extract to the datadir/refresh pipeline.
// Refresh downloads the two INSEE inputs (the per-ZE quarterly rates xlsx
// and the commune appartenance ZIP) and rebuilds the processed artifact via
// transform; Open resolves datadir > embed.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "chomage_zones_emploi.json"},
	Raw: []dataset.File{
		{Name: ratesRawName, URL: ratesURL},
		{Name: appartRawName, URL: appartURL},
	},
	Transform: transform,
	Validate:  validate,
}

// ZoneEntry carries one zone d'emploi's metadata + recent quarterly
// unemployment-rate series (oldest first). The series aligns 1:1 with
// Index.Quarters.
type ZoneEntry struct {
	Label   string    `json:"label,omitempty"`
	RatePct []float64 `json:"rate_pct"`
}

// Meta carries the manifest metadata for the embedded dataset.
type Meta struct {
	Source          string  `json:"source"`
	SeriesStart     string  `json:"series_start"`
	SeriesEnd       string  `json:"series_end"`
	QuarterCount    int     `json:"quarter_count"`
	ZECount         int     `json:"ze_count"`
	CommuneCount    int     `json:"commune_count"`
	NationalRatePct float64 `json:"national_rate_pct"`
	Note            string  `json:"note,omitempty"`
}

// Index is the per-commune-and-zone lookup index. INSEE → ZE2020 code,
// then ZE2020 code → labelled rate series.
type Index struct {
	Meta                  Meta                 `json:"meta"`
	Quarters              []string             `json:"quarters"`
	NationalRatePctSeries []float64            `json:"national_rate_pct_series"`
	Zones                 map[string]ZoneEntry `json:"zes"`
	Communes              map[string]string    `json:"communes"`
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
		return nil, fmt.Errorf("chomage: read embed: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("chomage: parse json: %w", err)
	}
	return &idx, nil
}

// LookupZE returns the zone d'emploi 2020 code for the given INSEE.
// `ok` is false when the commune is absent from the crosswalk.
func (idx *Index) LookupZE(insee string) (string, bool) {
	if idx == nil {
		return "", false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return "", false
	}
	ze, ok := idx.Communes[insee]
	return ze, ok
}

// LookupZone returns the rate-series entry for the given ZE2020 code.
// `ok` is false when the zone is absent (unexpected — every ZE in the
// crosswalk has a row in the series file).
func (idx *Index) LookupZone(zeCode string) (ZoneEntry, bool) {
	if idx == nil {
		return ZoneEntry{}, false
	}
	zeCode = strings.TrimSpace(zeCode)
	if zeCode == "" {
		return ZoneEntry{}, false
	}
	e, ok := idx.Zones[zeCode]
	return e, ok
}

// CommuneCount returns the number of communes in the embedded crosswalk.
func (idx *Index) CommuneCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

// ZoneCount returns the number of distinct zones d'emploi in the index.
func (idx *Index) ZoneCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.Zones)
}

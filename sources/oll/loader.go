package oll

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/oll.json
var embedFS embed.FS

// set binds the embedded snapshot to its upstream per-agglo archives (one raw
// per OLL agglomeration) so the datadir override and refresh tooling operate on
// it.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "oll.json"},
	Raw:       aggloRawFiles(),
	Transform: transform,
	Validate:  validate,
}

// aggloRawFiles lists one raw archive per configured agglomeration.
func aggloRawFiles() []dataset.File {
	out := make([]dataset.File, 0, len(aggloSpecs))
	for _, s := range aggloSpecs {
		out = append(out, dataset.File{Name: s.rawName(), URL: s.url()})
	}
	return out
}

// processed is the embedded artifact: one entry per OLL agglomeration covered.
type processed struct {
	Agglos []aggloData `json:"agglos"`
}

// aggloData is one observatory perimeter: its INSEE→zone map and its observed
// rent cells.
type aggloData struct {
	Code  string    `json:"code"`
	Name  string    `json:"name"`
	Year  int       `json:"year"`
	Zones []zoneRow `json:"zones"`
	Rents []rentRow `json:"rents"`
}

// zoneRow maps one commune to its OLL zone within the agglomeration.
type zoneRow struct {
	INSEE string `json:"insee"`
	Zone  string `json:"zone"`
	Label string `json:"label"`
}

// rentRow is one observed-rent cell: median/quartile €/m²/month for a
// (zone, rooms) bucket, with the sample size.
type rentRow struct {
	Zone           string  `json:"zone"`
	Pieces         int     `json:"pieces"`
	OpenEnded      bool    `json:"open_ended"`
	MedianEURPerM2 float64 `json:"median_eur_per_m2"`
	// ReletMedianEURPerM2 is the "emménagés récents" (< 1 an) median €/m²/month
	// HC — the level a landlord re-lets at. Observed when the cell publishes a
	// relet median, else derived from the zone/agglo relet-to-all ratio (see
	// parseRents). Zero when no relet signal is available for the cell.
	ReletMedianEURPerM2 float64 `json:"relet_median_eur_per_m2,omitempty"`
	Q1EURPerM2          float64 `json:"q1_eur_per_m2,omitempty"`
	Q3EURPerM2          float64 `json:"q3_eur_per_m2,omitempty"`
	SurfaceM2           float64 `json:"surface_m2,omitempty"`
	N                   int     `json:"n"`
}

// zoneRef is the resolved (agglo, zone) a commune belongs to.
type zoneRef struct {
	agglo string
	name  string
	zone  string
	label string
	year  int
}

// Index is the in-memory lookup built from the processed artifact.
type Index struct {
	inseeZone map[string]zoneRef // insee -> agglo+zone
	rents     map[string]rentRow // "agglo|zone|pieces" -> cell
}

func rentKey(agglo, zone string, pieces int) string {
	return fmt.Sprintf("%s|%s|%d", agglo, zone, pieces)
}

// Lookup resolves a commune + rooms bucket to its observed-rent cell. ok is
// false when the commune is outside every covered perimeter, or no cell exists
// for that rooms bucket.
func (idx *Index) Lookup(insee string, pieces int) (zoneRef, rentRow, bool) {
	if idx == nil {
		return zoneRef{}, rentRow{}, false
	}
	ref, ok := idx.inseeZone[insee]
	if !ok {
		return zoneRef{}, rentRow{}, false
	}
	cell, ok := idx.rents[rentKey(ref.agglo, ref.zone, pieces)]
	if !ok {
		return ref, rentRow{}, false
	}
	return ref, cell, true
}

// CommuneCount reports how many communes the index covers (across all agglos).
// Exposed for tests and operator diagnostics.
func (idx *Index) CommuneCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.inseeZone)
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton lookup index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. The
// dir from the first call wins for the process lifetime. A missing
// (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the plain-JSON artifact (oll.json is not gzipped) and
// builds the in-memory lookup maps.
func parseIndex(r io.Reader) (*Index, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	idx := &Index{inseeZone: map[string]zoneRef{}, rents: map[string]rentRow{}}
	if len(raw) == 0 {
		return idx, nil
	}
	var p processed
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	for _, a := range p.Agglos {
		for _, z := range a.Zones {
			idx.inseeZone[z.INSEE] = zoneRef{agglo: a.Code, name: a.Name, zone: z.Zone, label: z.Label, year: a.Year}
		}
		for _, r := range a.Rents {
			idx.rents[rentKey(a.Code, r.Zone, r.Pieces)] = r
		}
	}
	return idx, nil
}

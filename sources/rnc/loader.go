package rnc

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/rnc_coproprietes.json.gz
var embedFS embed.FS

// Name is the canonical Source identifier (registry + Dossier key).
const Name = "rnc"

// Version bumps when the Source's logic or payload shape changes.
const Version = 1

// set binds the embedded artifact to the datadir/refresh pipeline. Refresh
// downloads the upstream daily CSV and rebuilds the gzipped JSON via
// transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "rnc_coproprietes.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry is one copropriété row. The upstream omits financial and procedure
// fields, so they are absent here by construction.
type Entry struct {
	Immatriculation    string   `json:"imm"`
	NomUsage           string   `json:"nom,omitempty"`
	INSEE              string   `json:"insee"`
	Lat                float64  `json:"lat,omitempty"`
	Lon                float64  `json:"lon,omitempty"`
	VoieNorm           string   `json:"voie,omitempty"`    // normalized reference street
	VoiesComp          []string `json:"voies_c,omitempty"` // normalized complementary streets
	TypeSyndic         string   `json:"syndic,omitempty"`
	MandatEnCours      string   `json:"mandat,omitempty"`
	CoproAidee         bool     `json:"aidee,omitempty"`
	SyndicatCooperatif bool     `json:"coop,omitempty"`
	ResidenceService   bool     `json:"resserv,omitempty"`
	LotsTotal          int      `json:"lots,omitempty"`
	LotsHabitation     int      `json:"lotsh,omitempty"`
	ConstructionPeriod string   `json:"constr,omitempty"`
	QPVCode            string   `json:"qpv,omitempty"`
	QPVName            string   `json:"qpvn,omitempty"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source      string `json:"source"`
	DataVintage string `json:"data_vintage"`
	RowCount    int    `json:"row_count"`
}

// Index is the per-INSEE candidate lookup over the copro rows.
type Index struct {
	Meta    Meta             `json:"meta"`
	Copros  []Entry          `json:"copros"`
	ByInsee map[string][]int `json:"-"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, resolving from dir (datadir) with a
// fallback to the embedded artifact, parsing on first call. A missing
// non-embedded dataset yields an empty index (graceful degradation).
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			indexCache = &Index{}
			return
		}
		if err != nil {
			indexErr = fmt.Errorf("rnc: open dataset: %w", err)
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

func parseIndex(r io.Reader) (*Index, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("rnc: gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("rnc: read body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("rnc: parse json: %w", err)
	}
	idx.buildLookups()
	return &idx, nil
}

func (idx *Index) buildLookups() {
	idx.ByInsee = make(map[string][]int)
	for i := range idx.Copros {
		if e := idx.Copros[i]; e.INSEE != "" {
			idx.ByInsee[e.INSEE] = append(idx.ByInsee[e.INSEE], i)
		}
	}
}

// Count returns the number of copros in the index.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Copros)
}

// NewIndexForTest builds a ready-to-query index from in-memory rows. It is
// exported so downstream adapters (encheridor) can unit-test against a stub
// without the embedded national dataset.
func NewIndexForTest(copros []Entry) *Index {
	idx := &Index{Copros: copros}
	idx.buildLookups()
	return idx
}

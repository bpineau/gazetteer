package rnc

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/rnc_coproprietes.json.gz
var embedFS embed.FS

// Name is the canonical Source identifier (registry + Dossier key).
const Name = "rnc"

// Version bumps when the Source's logic or payload shape changes.
const Version = 2

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

// Entry is one copropriété row. The upstream open-data export omits the
// financial declarations and the legal-procedure/arrêté columns, so they are
// absent here by construction (see the package godoc).
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
	MandatFin          string   `json:"mandatfin,omitempty"` // date_fin_dernier_mandat (ISO YYYY-MM-DD)
	CoproAidee         bool     `json:"aidee,omitempty"`
	SyndicatCooperatif bool     `json:"coop,omitempty"`
	ResidenceService   bool     `json:"resserv,omitempty"`
	LotsTotal          int      `json:"lots,omitempty"`
	LotsHabitation     int      `json:"lotsh,omitempty"`
	LotsStationnement  int      `json:"lotsp,omitempty"`
	ConstructionPeriod string   `json:"constr,omitempty"`
	// Parcelles are the copropriété's cadastral parcel identifiers, each the
	// canonical 14-char French reference (INSEE+préfixe+section+numéro, e.g.
	// "75056102AG0011"). Newly published in the RNC export.
	Parcelles []string `json:"parc,omitempty"`
	DansACV   bool     `json:"acv,omitempty"` // copro_dans_acv (Action cœur de ville)
	DansPVD   bool     `json:"pvd,omitempty"` // copro_dans_pvd (Petites villes de demain)
	DansPDP   bool     `json:"pdp,omitempty"` // copro_dans_pdp
	QPVCode   string   `json:"qpv,omitempty"`
	QPVName   string   `json:"qpvn,omitempty"`
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

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton national index, resolving from dir (datadir)
// with a fallback to the embedded artifact, parsing on first call. A missing
// non-embedded dataset yields an empty index (graceful degradation).
//
// Dept-filtered loads (rnc.Options.Depts) do NOT share this singleton — see
// (*Source).index — because the resident set then depends on the filter.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, func(r io.Reader) (*Index, error) {
		return parseIndexStream(r, nil)
	})
}

// parseIndexStream decodes the gzipped national artifact one Entry at a time
// (a json.Decoder token walk over the "copros" array) rather than
// materialising the whole 648k-row slice before use. When depts is non-empty
// it keeps only rows whose INSEE lies in one of those departments, so the peak
// resident set for a filtered load never exceeds the filtered subset. Repeated
// low-cardinality strings (department INSEE, syndic type, mandate status,
// construction period, QPV identity) are interned to collapse the ~500 MB
// national footprint.
func parseIndexStream(r io.Reader, depts []string) (*Index, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%s: gunzip: %w", Name, err)
	}
	defer func() { _ = zr.Close() }()

	dec := json.NewDecoder(zr)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("%s: parse json: %w", Name, err)
	}

	idx := &Index{}
	keep := deptMatcher(depts)
	pool := make(map[string]string)
	intern := func(s string) string {
		if s == "" {
			return s
		}
		if v, ok := pool[s]; ok {
			return v
		}
		pool[s] = s
		return s
	}

	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: parse json: %w", Name, err)
		}
		switch key {
		case "meta":
			if err := dec.Decode(&idx.Meta); err != nil {
				return nil, fmt.Errorf("%s: parse meta: %w", Name, err)
			}
		case "copros":
			if err := expectDelim(dec, '['); err != nil {
				return nil, fmt.Errorf("%s: parse copros: %w", Name, err)
			}
			for dec.More() {
				var e Entry
				if err := dec.Decode(&e); err != nil {
					return nil, fmt.Errorf("%s: parse copro: %w", Name, err)
				}
				if keep != nil && !keep(e.INSEE) {
					continue
				}
				e.INSEE = intern(e.INSEE)
				e.TypeSyndic = intern(e.TypeSyndic)
				e.MandatEnCours = intern(e.MandatEnCours)
				e.ConstructionPeriod = intern(e.ConstructionPeriod)
				e.QPVCode = intern(e.QPVCode)
				e.QPVName = intern(e.QPVName)
				idx.Copros = append(idx.Copros, e)
			}
			if err := expectDelim(dec, ']'); err != nil {
				return nil, fmt.Errorf("%s: parse copros end: %w", Name, err)
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("%s: skip %v: %w", key, key, err)
			}
		}
	}
	idx.buildLookups()
	return idx, nil
}

// expectDelim reads the next token and asserts it is the given JSON delimiter.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, tok)
	}
	return nil
}

// deptMatcher returns a predicate keeping only INSEE codes in one of depts, or
// nil (keep everything) when depts is empty. Matching is by INSEE prefix, so
// 2-digit metropolitan departments and 3-digit DOM codes both work.
func deptMatcher(depts []string) func(insee string) bool {
	if len(depts) == 0 {
		return nil
	}
	prefixes := make([]string, 0, len(depts))
	for _, d := range depts {
		if d = strings.TrimSpace(d); d != "" {
			prefixes = append(prefixes, d)
		}
	}
	if len(prefixes) == 0 {
		return nil
	}
	return func(insee string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(insee, p) {
				return true
			}
		}
		return false
	}
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

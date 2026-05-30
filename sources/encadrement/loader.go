package encadrement

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

//go:embed data/encadrement_paris.json data/encadrement_plaine_commune.json data/encadrement_lyon_villeurbanne.json
var embedFS embed.FS

// This Source ships three embedded zone extracts (Paris, Plaine Commune,
// Lyon/Villeurbanne); each is its own dataset.Set so the datadir override
// and the refresh tooling operate per file. Each has its own raw upstream
// and Transform that rebuilds its committed JSON array (see transform.go).
var (
	setParis = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_paris.json"},
		Raw:       []dataset.File{{Name: rawParisName, URL: rawParisURL}},
		Transform: transformParis,
		Validate:  validateParis,
	}
	setPlaineCommune = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_plaine_commune.json"},
		Raw:       []dataset.File{{Name: rawPlaineCommuneName, URL: rawPlaineCommuneURL}},
		Transform: transformPlaineCommune,
		Validate:  validatePlaineCommune,
	}
	setLyon = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_lyon_villeurbanne.json"},
		Raw:       []dataset.File{{Name: rawLyonName, URL: rawLyonURL}},
		Transform: transformLyon,
		Validate:  validateLyon,
	}
)

// Entry is the canonical per-cell shape across all zones encadrées.
// The flat representation absorbs the per-zone JSON quirks (Paris
// quartier × époque × pièces, Plaine Commune zone × pièces × époque,
// Lyon IRIS × ...) into a single lookup table.
type Entry struct {
	// ZoneSource is the dataset name ("paris" | "plaine_commune" |
	// "lyon_villeurbanne").
	ZoneSource string

	// ZoneID identifies the geographic cell inside the source — for
	// Paris it's the code_grand_quartier (7-digit), Plaine Commune the
	// "zone" number, Lyon the IRIS code.
	ZoneID string

	// Arrondissement is the 2-digit Paris arrondissement (01-20)
	// extracted from code_grand_quartier. Empty outside Paris.
	Arrondissement string

	// Commune is the human-readable label, when the source provides it.
	Commune string

	// Piece is the nombre de pièces bucket (1, 2, 3, 4...). The
	// "et plus" open-ended cell is represented by Piece = 4 (the
	// source stops at 4 in Plaine Commune; Lyon goes 1..4 as well).
	Piece int

	// PieceOpenEnded is true when the cell covers "Piece et plus".
	PieceOpenEnded bool

	// Epoque is the construction-period bucket ("avant 1946",
	// "1946-1970", ...). The vocabulary varies by source; matchers do
	// fuzzy comparison.
	Epoque string

	// Meuble distinguishes meublé from non-meublé cells. The persisted
	// score is always computed against Meuble=false (loyer nu).
	Meuble bool

	// Maison marks the rare cells reserved for maisons individuelles
	// (Plaine Commune publishes them separately). False for the
	// default apartment cells.
	Maison bool

	// LoyerRefEURPerM2HC is the loyer de référence, EUR/m²/month HC.
	LoyerRefEURPerM2HC float64

	// LoyerRefMinEURPerM2HC and LoyerRefMaxEURPerM2HC are the legal
	// minoré / majoré bounds.
	LoyerRefMinEURPerM2HC float64
	LoyerRefMaxEURPerM2HC float64
}

// Index holds the lookup index for all encadrement datasets.
type Index struct {
	// byArrondissement groups Paris entries by 2-digit arrondissement.
	byArrondissement map[string][]Entry

	// byPlaineCommuneZone groups Plaine Commune entries by zone string.
	byPlaineCommuneZone map[string][]Entry

	// byLyonIRIS groups Lyon / Villeurbanne entries by IRIS code.
	byLyonIRIS map[string][]Entry

	// byLyonInsee groups Lyon / Villeurbanne entries by commune INSEE
	// (used when the auction lacks an IRIS code).
	byLyonInsee map[string][]Entry
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton lookup index, resolving each zone artifact
// from dir (the datadir) with a fallback to the embedded copies and parsing
// them on first call. The dir from the first call wins for the process
// lifetime. A missing (non-embedded) zone contributes no entries rather
// than failing the whole index.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		indexCache, indexErr = parseAll(dir)
	})
	return indexCache, indexErr
}

// readSet returns the bytes of a zone artifact, or nil when it is neither
// in the datadir nor embedded (graceful: that zone simply contributes no
// rows).
func readSet(s dataset.Set, dir string) ([]byte, error) {
	rc, err := s.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// unmarshalRows decodes a JSON array into dst, treating empty input (an
// absent zone) as zero rows rather than a parse error.
func unmarshalRows(raw []byte, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// LookupParis returns every cell published for the given arrondissement.
// arrondissement is 2-digit zero-padded ("01" .. "20"). Empty slice
// when the arrondissement is unknown.
func (idx *Index) LookupParis(arrondissement string) []Entry {
	if idx == nil {
		return nil
	}
	return idx.byArrondissement[arrondissement]
}

// LookupPlaineCommuneZone returns every cell published for the given
// Plaine Commune zone (stringified, e.g. "310"). Empty slice when the
// zone is unknown.
func (idx *Index) LookupPlaineCommuneZone(zone string) []Entry {
	if idx == nil {
		return nil
	}
	return idx.byPlaineCommuneZone[zone]
}

// LookupLyonIRIS returns every cell published for the given IRIS code.
// Empty slice when the IRIS is not in the Métropole de Lyon perimeter.
func (idx *Index) LookupLyonIRIS(iris string) []Entry {
	if idx == nil {
		return nil
	}
	return idx.byLyonIRIS[iris]
}

// LookupLyonInsee returns every cell published for any IRIS inside the
// given INSEE commune (Lyon arrondissements + Villeurbanne). Used when
// the auction lacks an IRIS code.
func (idx *Index) LookupLyonInsee(insee string) []Entry {
	if idx == nil {
		return nil
	}
	return idx.byLyonInsee[insee]
}

// CountParis / CountPlaineCommune / CountLyon expose row counts for
// tests and operator-facing tools.
func (idx *Index) CountParis() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, v := range idx.byArrondissement {
		n += len(v)
	}
	return n
}

// CountPlaineCommune returns the total Plaine Commune cell count.
func (idx *Index) CountPlaineCommune() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, v := range idx.byPlaineCommuneZone {
		n += len(v)
	}
	return n
}

// CountLyon returns the total Lyon / Villeurbanne cell count.
func (idx *Index) CountLyon() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, v := range idx.byLyonIRIS {
		n += len(v)
	}
	return n
}

// Raw JSON shapes ------------------------------------------------------

type parisRow struct {
	Annee             int     `json:"annee"`
	IDZone            int     `json:"id_zone"`
	IDQuartier        int     `json:"id_quartier"`
	NomQuartier       string  `json:"nom_quartier"`
	CodeGrandQuartier int     `json:"code_grand_quartier"`
	Piece             int     `json:"piece"`
	Epoque            string  `json:"epoque"`
	Meuble            bool    `json:"meuble"`
	RefEURPerM2       float64 `json:"ref_eur_m2"`
	MinEURPerM2       float64 `json:"min_eur_m2"`
	MaxEURPerM2       float64 `json:"max_eur_m2"`
}

type plaineCommuneRow struct {
	Zone           int     `json:"zone"`
	Piece          int     `json:"piece"`
	PieceOpenEnded bool    `json:"piece_open_ended"`
	Epoque         string  `json:"epoque"`
	Meuble         bool    `json:"meuble"`
	Maison         bool    `json:"maison"`
	RefEURPerM2    float64 `json:"ref_eur_m2"`
	MinEURPerM2    float64 `json:"min_eur_m2"`
	MaxEURPerM2    float64 `json:"max_eur_m2"`
}

type lyonRow struct {
	Insee       string   `json:"insee"`
	IRIS        string   `json:"iris"`
	Zone        string   `json:"zone"`
	Commune     string   `json:"commune"`
	Piece       int      `json:"piece"`
	Epoque      string   `json:"epoque"`
	Meuble      bool     `json:"meuble"`
	RefEURPerM2 float64  `json:"ref_eur_m2"`
	MinEURPerM2 *float64 `json:"min_eur_m2"`
	MaxEURPerM2 *float64 `json:"max_eur_m2"`
}

func parseAll(dir string) (*Index, error) {
	idx := &Index{
		byArrondissement:    map[string][]Entry{},
		byPlaineCommuneZone: map[string][]Entry{},
		byLyonIRIS:          map[string][]Entry{},
		byLyonInsee:         map[string][]Entry{},
	}

	// Paris.
	raw, err := readSet(setParis, dir)
	if err != nil {
		return nil, fmt.Errorf("encadrement: read paris: %w", err)
	}
	var paris []parisRow
	if err := unmarshalRows(raw, &paris); err != nil {
		return nil, fmt.Errorf("encadrement: parse paris: %w", err)
	}
	for _, r := range paris {
		arr := parisArrondissement(r.CodeGrandQuartier)
		if arr == "" {
			continue
		}
		e := Entry{
			ZoneSource:            ZoneSourceParis,
			ZoneID:                fmt.Sprintf("%d", r.CodeGrandQuartier),
			Arrondissement:        arr,
			Commune:               r.NomQuartier,
			Piece:                 r.Piece,
			PieceOpenEnded:        false,
			Epoque:                r.Epoque,
			Meuble:                r.Meuble,
			LoyerRefEURPerM2HC:    r.RefEURPerM2,
			LoyerRefMinEURPerM2HC: r.MinEURPerM2,
			LoyerRefMaxEURPerM2HC: r.MaxEURPerM2,
		}
		idx.byArrondissement[arr] = append(idx.byArrondissement[arr], e)
	}

	// Plaine Commune.
	raw, err = readSet(setPlaineCommune, dir)
	if err != nil {
		return nil, fmt.Errorf("encadrement: read plaine commune: %w", err)
	}
	var pc []plaineCommuneRow
	if err := unmarshalRows(raw, &pc); err != nil {
		return nil, fmt.Errorf("encadrement: parse plaine commune: %w", err)
	}
	for _, r := range pc {
		zone := fmt.Sprintf("%d", r.Zone)
		e := Entry{
			ZoneSource:            ZoneSourcePlaineCommune,
			ZoneID:                zone,
			Piece:                 r.Piece,
			PieceOpenEnded:        r.PieceOpenEnded,
			Epoque:                r.Epoque,
			Meuble:                r.Meuble,
			Maison:                r.Maison,
			LoyerRefEURPerM2HC:    r.RefEURPerM2,
			LoyerRefMinEURPerM2HC: r.MinEURPerM2,
			LoyerRefMaxEURPerM2HC: r.MaxEURPerM2,
		}
		idx.byPlaineCommuneZone[zone] = append(idx.byPlaineCommuneZone[zone], e)
	}

	// Lyon / Villeurbanne.
	raw, err = readSet(setLyon, dir)
	if err != nil {
		return nil, fmt.Errorf("encadrement: read lyon: %w", err)
	}
	var lyon []lyonRow
	if err := unmarshalRows(raw, &lyon); err != nil {
		return nil, fmt.Errorf("encadrement: parse lyon: %w", err)
	}
	for _, r := range lyon {
		var mn, mx float64
		if r.MinEURPerM2 != nil {
			mn = *r.MinEURPerM2
		}
		if r.MaxEURPerM2 != nil {
			mx = *r.MaxEURPerM2
		}
		e := Entry{
			ZoneSource:            ZoneSourceLyonVilleurbanne,
			ZoneID:                r.IRIS,
			Commune:               r.Commune,
			Piece:                 r.Piece,
			Epoque:                r.Epoque,
			Meuble:                r.Meuble,
			LoyerRefEURPerM2HC:    r.RefEURPerM2,
			LoyerRefMinEURPerM2HC: mn,
			LoyerRefMaxEURPerM2HC: mx,
		}
		if r.IRIS != "" {
			idx.byLyonIRIS[r.IRIS] = append(idx.byLyonIRIS[r.IRIS], e)
		}
		if r.Insee != "" {
			idx.byLyonInsee[r.Insee] = append(idx.byLyonInsee[r.Insee], e)
		}
	}

	return idx, nil
}

// parisArrondissement extracts the 2-digit Paris arrondissement from a
// code_grand_quartier (7-digit, starting with 7510 or 7511 or 7512).
// Returns "" when the code is malformed.
//
// Layout (per INSEE conventions) : 751<AA><QQ>
//   - 751   : Paris insee prefix
//   - AA    : 2-digit arrondissement (01-20)
//   - QQ    : 3-digit quartier id (rare combinations omit a digit, so we
//     rely on the AA position rather than total length).
//
// In practice every published cell carries a 7-digit code where the
// AA digits are at index 3..4.
func parisArrondissement(code int) string {
	s := fmt.Sprintf("%d", code)
	if !strings.HasPrefix(s, "751") || len(s) < 5 {
		return ""
	}
	return s[3:5]
}

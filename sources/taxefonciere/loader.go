package taxefonciere

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/taxe_fonciere_ratios.json data/fiscalite_locale.json
var embedFS embed.FS

// This Source ships two embedded artifacts (V1 legacy ratios + V2 DGFiP
// voted rates); each is its own dataset.Set so the datadir override and the
// refresh tooling operate per file. Both are refreshable from their
// data.economie.gouv.fr Opendatasoft exports (see transform.go).
var (
	setV1 = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "taxe_fonciere_ratios.json"},
		Raw:       []dataset.File{{Name: rawV1Name, URL: rawV1URL}},
		Transform: transformV1,
		Validate:  validateV1,
	}
	setV2 = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "fiscalite_locale.json"},
		Raw:       []dataset.File{{Name: rawV2Name, URL: rawV2URL}},
		Transform: transformV2,
		Validate:  validateV2,
	}
)

// V1Index carries the per-commune and dept-fallback "valeur locative
// cadastrale" ratios sourced from the DGFiP "Tarifs des locaux
// d'habitation" dataset (legacy). The value stored is EUR/m² (annual)
// representing TF dûe par m² après abattement.
type V1Index struct {
	Meta struct {
		Source string `json:"source"`
		Unit   string `json:"unit"`
		Note   string `json:"note"`
	} `json:"meta"`
	Communes     map[string]float64 `json:"communes"`
	DeptFallback map[string]float64 `json:"dept_fallback"`
}

// V2Entry carries the voted TFPB rate and the TEOM rate for a single
// commune (or department fallback). Both rates are expressed in percent
// of the valeur locative cadastrale and are typically in the
// 20–55 % band for TFPB and 5–20 % band for TEOM.
type V2Entry struct {
	TFPBPct float64 `json:"tfpb_pct,omitempty"`
	TEOMPct float64 `json:"teom_pct,omitempty"`
}

// V2Index carries the DGFiP "Fiscalité locale des particuliers"
// (exercice 2025) voted taxes per commune INSEE, with a dept-median
// fallback when the commune is missing. The Meta block bundles the
// per-m² VLC tariff proxy and the abattement percentage used by the
// V2 estimator.
type V2Index struct {
	Meta struct {
		Source            string  `json:"source"`
		DownloadedAt      string  `json:"downloaded_at"`
		DataYear          int     `json:"data_year"`
		RowCountCommunes  int     `json:"row_count_communes"`
		RowCountDepts     int     `json:"row_count_depts"`
		Note              string  `json:"note"`
		VLCTariffEURPerM2 float64 `json:"vlc_tariff_eur_per_m2_year"`
		VLCAbattement     float64 `json:"vlc_abattement"`
	} `json:"meta"`
	Communes     map[string]V2Entry `json:"communes"`
	DeptFallback map[string]V2Entry `json:"dept_fallback"`
}

// Index bundles both V1 (legacy ratio) and V2 (DGFiP taux votés)
// datasets. The Source consults V2 first and falls back to V1 only
// when V2 returns no signal at all.
type Index struct {
	V1 *V1Index
	V2 *V2Index
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton lookup index, resolving both processed
// artifacts from dir (the datadir) with a fallback to the embedded copies
// and parsing them on first call. The dir from the first call wins for the
// process lifetime. A missing (non-embedded) artifact yields an empty
// sub-index rather than an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		v1, err := loadV1(dir)
		if err != nil {
			indexErr = err
			return
		}
		v2, err := loadV2(dir)
		if err != nil {
			indexErr = err
			return
		}
		indexCache = &Index{V1: v1, V2: v2}
	})
	return indexCache, indexErr
}

func loadV1(dir string) (*V1Index, error) {
	rc, err := setV1.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return &V1Index{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("taxefonciere: open taxe_fonciere_ratios: %w", err)
	}
	defer func() { _ = rc.Close() }()
	var idx V1Index
	if err := json.NewDecoder(rc).Decode(&idx); err != nil {
		return nil, fmt.Errorf("taxefonciere: parse taxe_fonciere_ratios: %w", err)
	}
	return &idx, nil
}

func loadV2(dir string) (*V2Index, error) {
	rc, err := setV2.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		idx := &V2Index{}
		applyV2Defaults(idx)
		return idx, nil
	}
	if err != nil {
		return nil, fmt.Errorf("taxefonciere: open fiscalite_locale: %w", err)
	}
	defer func() { _ = rc.Close() }()
	var idx V2Index
	if err := json.NewDecoder(rc).Decode(&idx); err != nil {
		return nil, fmt.Errorf("taxefonciere: parse fiscalite_locale: %w", err)
	}
	applyV2Defaults(&idx)
	return &idx, nil
}

// applyV2Defaults backstops the VLC tariff + abattement when the upstream
// extract omits them, keeping the V2 estimator usable.
func applyV2Defaults(idx *V2Index) {
	if idx.Meta.VLCTariffEURPerM2 <= 0 {
		// Default backstop: 90 €/m²/an matches the national median
		// VLC moyenne cited in the DGFiP "Tarifs des locaux
		// d'habitation" notice (residential cats 4-6).
		idx.Meta.VLCTariffEURPerM2 = 90.0
	}
	if idx.Meta.VLCAbattement <= 0 {
		idx.Meta.VLCAbattement = 0.5 // CGI art. 1388.
	}
}

// LookupV1 returns the V1 EUR/m² ratio for the commune (or dept
// median). The `usedFallback` flag flips true on a dept fallback.
// `ok` is false when even the dept is missing (DOM-TOM, Mayotte).
func (idx *V1Index) LookupV1(insee string) (vl float64, usedFallback bool, ok bool) {
	if idx == nil {
		return 0, false, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return 0, false, false
	}
	if v, hit := idx.Communes[insee]; hit {
		return v, false, true
	}
	dept := deptFromInsee(insee)
	if dept == "" {
		return 0, false, false
	}
	if v, hit := idx.DeptFallback[dept]; hit {
		return v, true, true
	}
	return 0, false, false
}

// LookupV2 returns the (TFPB, TEOM) couple for the commune.
func (idx *V2Index) LookupV2(insee string) (entry V2Entry, usedFallback bool, ok bool) {
	if idx == nil {
		return V2Entry{}, false, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return V2Entry{}, false, false
	}
	if v, hit := idx.Communes[insee]; hit {
		return v, false, true
	}
	dept := deptFromInsee(insee)
	if dept == "" {
		return V2Entry{}, false, false
	}
	if v, hit := idx.DeptFallback[dept]; hit {
		return v, true, true
	}
	return V2Entry{}, false, false
}

// CountCommunesV1 returns the number of communes in the V1 index.
func (idx *V1Index) CountCommunesV1() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

// CountCommunesV2 returns the number of communes in the V2 index.
func (idx *V2Index) CountCommunesV2() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}

// CountDeptsV1 returns the number of departments in the V1 fallback.
func (idx *V1Index) CountDeptsV1() int {
	if idx == nil {
		return 0
	}
	return len(idx.DeptFallback)
}

// CountDeptsV2 returns the number of departments in the V2 fallback.
func (idx *V2Index) CountDeptsV2() int {
	if idx == nil {
		return 0
	}
	return len(idx.DeptFallback)
}

// deptFromInsee extracts the dept code from an INSEE commune code.
// Corsica uses 2A / 2B (3-char dept). DOM-TOM use 3-digit (971..976).
// Métropole uses 2-digit (01..95).
func deptFromInsee(insee string) string {
	if len(insee) < 2 {
		return ""
	}
	if strings.HasPrefix(insee, "2A") || strings.HasPrefix(insee, "2B") {
		return insee[:2]
	}
	if strings.HasPrefix(insee, "97") || strings.HasPrefix(insee, "98") {
		if len(insee) >= 3 {
			return insee[:3]
		}
		return insee[:2]
	}
	return insee[:2]
}

package encadrement

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
)

//go:embed data/encadrement_paris.json data/encadrement_plaine_commune.json data/encadrement_lyon_villeurbanne.json data/encadrement_est_ensemble.json data/encadrement_plaine_commune_zones.json data/encadrement_est_ensemble_zones.json
var embedFS embed.FS

// This Source ships, as independent dataset.Sets, the barème extracts (Paris,
// Plaine Commune, Est Ensemble, Lyon/Villeurbanne) plus the Seine-Saint-Denis
// zonage geometry (Plaine Commune, Est Ensemble). Each is its own Set so the
// datadir override and the refresh tooling operate per file; each has its own
// raw upstream and Transform that rebuilds its committed artifact (transform.go).
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
		Raw:       eptRawFiles(eptRawNamePlaineCommune, rawPlaineCommuneKMLBase),
		Transform: transformPlaineCommune,
		Validate:  validatePlaineCommune,
	}
	setEstEnsemble = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_est_ensemble.json"},
		Raw:       eptRawFiles(eptRawNameEstEnsemble, rawEstEnsembleKMLBase),
		Transform: transformEstEnsemble,
		Validate:  validateEstEnsemble,
	}
	setPlaineCommuneZones = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_plaine_commune_zones.json"},
		Raw:       []dataset.File{{Name: rawPlaineCommuneZonesName, URL: rawPlaineCommuneZonesURL}},
		Transform: transformPlaineCommuneZones,
		Validate:  validateZones,
	}
	setEstEnsembleZones = dataset.Set{
		Source:    Name,
		Version:   Version,
		Embed:     embedFS,
		Processed: dataset.File{Name: "encadrement_est_ensemble_zones.json"},
		Raw:       []dataset.File{{Name: rawEstEnsembleZonesName, URL: rawEstEnsembleZonesURL}},
		Transform: transformEstEnsembleZones,
		Validate:  validateZones,
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
	// "est_ensemble" | "lyon_villeurbanne").
	ZoneSource string

	// ZoneID identifies the geographic cell inside the source — for
	// Paris it's the code_grand_quartier (7-digit), Plaine Commune /
	// Est Ensemble the "zone" number, Lyon the IRIS code.
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

	// byEstEnsembleZone groups Est Ensemble entries by zone string.
	byEstEnsembleZone map[string][]Entry

	// byLyonIRIS groups Lyon / Villeurbanne entries by IRIS code.
	byLyonIRIS map[string][]Entry

	// byLyonInsee groups Lyon / Villeurbanne entries by commune INSEE
	// (used when the auction lacks an IRIS code).
	byLyonInsee map[string][]Entry

	// zones holds the embedded zonage geometry for the Seine-Saint-Denis EPTs
	// (Plaine Commune, Est Ensemble), as a geoindex whose payload is the zone
	// identity — used to resolve a coordinate to its zone.
	zones *geoindex.Index[zoneID]

	// inseeEPT maps each EPT commune INSEE to its EPT (zone_source) — the
	// membership test for the Seine-Saint-Denis resolution branch.
	inseeEPT map[string]string

	// inseeCommune maps each EPT commune INSEE to its human-readable name.
	inseeCommune map[string]string

	// inseeZones maps each EPT commune INSEE to the distinct zones (sorted)
	// covering it. A single-zone commune resolves without coordinates; a
	// multi-zone one (Saint-Denis, Montreuil) needs the point-in-polygon path.
	inseeZones map[string][]string
}

// zoneID is the per-feature payload carried by the zonage geoindex: a zone's
// EPT, zone number, hosting-commune INSEE and human-readable name.
type zoneID struct {
	ept     string
	zone    string
	insee   string
	commune string
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

// LookupEstEnsembleZone returns every cell published for the given Est Ensemble
// zone (stringified, e.g. "307"). Empty slice when the zone is unknown.
func (idx *Index) LookupEstEnsembleZone(zone string) []Entry {
	if idx == nil {
		return nil
	}
	return idx.byEstEnsembleZone[zone]
}

// CountEstEnsemble returns the total Est Ensemble cell count.
func (idx *Index) CountEstEnsemble() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, v := range idx.byEstEnsembleZone {
		n += len(v)
	}
	return n
}

// ZonesForINSEE returns the encadrement zone identifiers covering the
// given EPT commune (Plaine Commune / Est Ensemble) and the owning
// dataset (ZoneSourcePlaineCommune | ZoneSourceEstEnsemble), straight
// from the embedded zonage artifacts — so batch consumers (overview)
// track perimeter revisions without hand-maintained commune tables.
// A multi-zone commune (Saint-Denis, Montreuil) returns several zones,
// sorted. ok is false for communes outside both EPTs (Paris and Lyon are
// keyed by arrondissement / IRIS instead — see LookupParis, LookupLyonInsee).
func (idx *Index) ZonesForINSEE(insee string) (zones []string, zoneSource string, ok bool) {
	if idx == nil {
		return nil, "", false
	}
	ept, ok := idx.inseeEPT[insee]
	if !ok {
		return nil, "", false
	}
	return idx.inseeZones[insee], ept, true
}

// LookupEPTZone returns the barème cells for a (EPT, zone) pair, where
// ept is ZoneSourcePlaineCommune or ZoneSourceEstEnsemble. Zone numbers
// are not unique across EPTs, so the EPT must scope the lookup. Empty
// for an unknown pair.
func (idx *Index) LookupEPTZone(ept, zone string) []Entry {
	if idx == nil {
		return nil
	}
	switch ept {
	case ZoneSourcePlaineCommune:
		return idx.byPlaineCommuneZone[zone]
	case ZoneSourceEstEnsemble:
		return idx.byEstEnsembleZone[zone]
	default:
		return nil
	}
}

// ept93Match is the outcome of resolving a Seine-Saint-Denis listing to its
// rent-control zone(s).
type ept93Match struct {
	ept     string   // ZoneSourcePlaineCommune | ZoneSourceEstEnsemble
	zones   []string // exactly one (precise / single-zone) or every commune zone
	commune string   // human-readable label
	conf    string   // ConfidenceMedium (resolved) | ConfidenceLow (ambiguous)
}

// resolve93 resolves a listing in the Plaine Commune / Est Ensemble perimeter to
// its zone(s). ok is false when the INSEE is not an EPT commune.
//
//   - With usable coordinates: point-in-polygon over the listing's own commune
//     picks the exact zone (Medium). Candidates are scoped to the listing's
//     INSEE, so a coordinate that drifts into a neighbouring commune cannot
//     silently override the stated commune.
//   - Without coordinates, or when the point falls outside the commune's
//     polygons (a geocoding sliver near a boundary): a single-zone commune
//     resolves to that zone (Medium); a multi-zone commune (Saint-Denis,
//     Montreuil) is ambiguous and collapses across all its zones (Low).
func (idx *Index) resolve93(insee string, lat, lon *float64) (ept93Match, bool) {
	ept := idx.inseeEPT[insee]
	if ept == "" {
		return ept93Match{}, false
	}
	commune := idx.inseeCommune[insee]

	// (0,0) is the "unset coords" sentinel: a real listing never sits on Null
	// Island, and every EPT commune is far from it, so this never rejects a
	// genuine coordinate here.
	if lat != nil && lon != nil && !(*lat == 0 && *lon == 0) {
		// Scope the point-in-polygon scan to the listing's own commune, so a
		// coordinate that drifts into a neighbouring commune cannot override the
		// stated commune.
		if za, ok := idx.zones.ResolveWhere(*lat, *lon, func(z zoneID) bool { return z.insee == insee }); ok {
			return ept93Match{
				ept:     ept,
				zones:   []string{za.zone},
				commune: firstNonEmpty(za.commune, commune),
				conf:    ConfidenceMedium,
			}, true
		}
	}

	switch zones := idx.inseeZones[insee]; len(zones) {
	case 0:
		return ept93Match{}, false
	case 1:
		return ept93Match{ept: ept, zones: zones, commune: commune, conf: ConfidenceMedium}, true
	default:
		return ept93Match{ept: ept, zones: zones, commune: commune, conf: ConfidenceLow}, true
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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

// eptBaremeRow is the processed per-cell shape shared by the two
// Seine-Saint-Denis EPT barèmes (Plaine Commune, Est Ensemble) — identical
// schema, so one row type and one transform serve both.
type eptBaremeRow struct {
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

// zoneRow is one feature of an embedded EPT zonage artifact: a geographic
// rent-control zone, the commune it belongs to, and its boundary geometry.
// Polygons is [polygon][ring][vertex][lon,lat], evaluated under geopoly's
// even-odd rule (conventionally ring 0 is the outer boundary and further rings
// are holes, but some upstream features pack several disjoint regions into one
// Polygon's rings — geopoly unions those; see geopoly.Polygon).
type zoneRow struct {
	EPT      string           `json:"ept"`
	Zone     string           `json:"zone"`
	INSEE    string           `json:"insee"`
	Commune  string           `json:"commune"`
	Polygons geoindex.Compact `json:"polygons"`
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
		byEstEnsembleZone:   map[string][]Entry{},
		byLyonIRIS:          map[string][]Entry{},
		byLyonInsee:         map[string][]Entry{},
		inseeEPT:            map[string]string{},
		inseeCommune:        map[string]string{},
		inseeZones:          map[string][]string{},
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
	if err := loadEPTBareme(setPlaineCommune, dir, "plaine commune", ZoneSourcePlaineCommune, idx.byPlaineCommuneZone); err != nil {
		return nil, err
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

	// Est Ensemble (same processed barème schema as Plaine Commune).
	if err := loadEPTBareme(setEstEnsemble, dir, "est ensemble", ZoneSourceEstEnsemble, idx.byEstEnsembleZone); err != nil {
		return nil, err
	}

	// Seine-Saint-Denis zonage geometry (Plaine Commune + Est Ensemble).
	var zoneFeats []geoindex.Feature[zoneID]
	var zoneIDs []zoneID
	for _, zs := range []dataset.Set{setPlaineCommuneZones, setEstEnsembleZones} {
		raw, err = readSet(zs, dir)
		if err != nil {
			return nil, fmt.Errorf("encadrement: read %s: %w", zs.Processed.Name, err)
		}
		var rows []zoneRow
		if err := unmarshalRows(raw, &rows); err != nil {
			return nil, fmt.Errorf("encadrement: parse %s: %w", zs.Processed.Name, err)
		}
		for _, z := range rows {
			id := idx.addZone(z)
			zoneFeats = append(zoneFeats, geoindex.NewFeature(id, z.Polygons.MultiPolygon()))
			zoneIDs = append(zoneIDs, id)
		}
	}
	idx.zones = geoindex.New(zoneFeats)
	idx.finalizeZones(zoneIDs)

	return idx, nil
}

// loadEPTBareme reads one Seine-Saint-Denis EPT barème artifact (Plaine
// Commune and Est Ensemble share the eptBaremeRow schema) and projects its
// rows — stamped with zoneSource — into the per-zone lookup map. label names
// the EPT in error messages.
func loadEPTBareme(s dataset.Set, dir, label, zoneSource string, byZone map[string][]Entry) error {
	raw, err := readSet(s, dir)
	if err != nil {
		return fmt.Errorf("encadrement: read %s: %w", label, err)
	}
	var rows []eptBaremeRow
	if err := unmarshalRows(raw, &rows); err != nil {
		return fmt.Errorf("encadrement: parse %s: %w", label, err)
	}
	for _, r := range rows {
		zone := fmt.Sprintf("%d", r.Zone)
		byZone[zone] = append(byZone[zone], Entry{
			ZoneSource:            zoneSource,
			ZoneID:                zone,
			Piece:                 r.Piece,
			PieceOpenEnded:        r.PieceOpenEnded,
			Epoque:                r.Epoque,
			Meuble:                r.Meuble,
			Maison:                r.Maison,
			LoyerRefEURPerM2HC:    r.RefEURPerM2,
			LoyerRefMinEURPerM2HC: r.MinEURPerM2,
			LoyerRefMaxEURPerM2HC: r.MaxEURPerM2,
		})
	}
	return nil
}

// addZone records one zonage feature's EPT/commune membership in the lookup
// maps and returns its identity payload (the caller builds the geoindex
// feature from it plus the geometry).
func (idx *Index) addZone(z zoneRow) zoneID {
	if z.INSEE != "" {
		if idx.inseeEPT[z.INSEE] == "" {
			idx.inseeEPT[z.INSEE] = z.EPT
		}
		if z.Commune != "" {
			idx.inseeCommune[z.INSEE] = z.Commune
		}
	}
	return zoneID{ept: z.EPT, zone: z.Zone, insee: z.INSEE, commune: z.Commune}
}

// finalizeZones derives the per-commune distinct-zone lists from the loaded
// zone identities, so resolve93 can tell single-zone communes (resolvable
// without coordinates) from multi-zone ones.
func (idx *Index) finalizeZones(zones []zoneID) {
	tmp := map[string]map[string]struct{}{}
	for _, za := range zones {
		if tmp[za.insee] == nil {
			tmp[za.insee] = map[string]struct{}{}
		}
		tmp[za.insee][za.zone] = struct{}{}
	}
	for insee, set := range tmp {
		zs := make([]string, 0, len(set))
		for z := range set {
			zs = append(zs, z)
		}
		sort.Strings(zs)
		idx.inseeZones[insee] = zs
	}
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

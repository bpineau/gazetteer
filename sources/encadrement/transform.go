package encadrement

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
)

// Raw input filenames (datadir basenames) and upstream URLs --------------
//
// Each city is an independent dataset.Set with its own raw input and
// Transform. The committed JSON arrays are reproduced byte-equivalently
// from these upstreams (proven via the python diffs in the design notes):
//
//   - Paris: opendata.paris.fr JSON export, filtered to parisYear.
//   - Plaine Commune / Est Ensemble: the DRIHL référence-loyer KML barème
//     (one KML file per logement × pièces × époque × meublé cell, each
//     carrying every zone as a Placemark with ref/refmin/refmaj) — this is
//     the authoritative, annually-renewed source (data.gouv only mirrors an
//     obsolete 2022/2023 flat export). See eptBaremeVintage.
//   - Lyon/Villeurbanne: a data.grandlyon.com WFS GeoJSON whose per-IRIS
//     "valeurs" object nests piece × époque × meublé cells.
const (
	rawParisName = "encadrement_paris.raw.json"
	rawParisURL  = "https://opendata.paris.fr/api/explore/v2.1/catalog/datasets/logement-encadrement-des-loyers/exports/json"

	// eptBaremeVintage is the DRIHL arrêté period the EPT barèmes are drawn
	// from ("du 01 juin 2026 au 31 mai 2027"); it selects the KML directory.
	// eptBaremeYear is its year, gated by the vintage legality test so a stale
	// arrêté can never ship silently again.
	eptBaremeVintage = "2026-06-01"
	eptBaremeYear    = 2026

	rawEstEnsembleKMLBase   = "http://www.referenceloyer.drihl.ile-de-france.developpement-durable.gouv.fr/est-ensemble/kml/" + eptBaremeVintage
	rawPlaineCommuneKMLBase = "http://www.referenceloyer.drihl.ile-de-france.developpement-durable.gouv.fr/plaine-commune/kml/" + eptBaremeVintage

	// eptRawNamePlaineCommune / eptRawNameEstEnsemble prefix the per-cell KML
	// datadir basenames (both EPTs share the "encadrement" datadir).
	eptRawNamePlaineCommune = "encadrement_plaine_commune"
	eptRawNameEstEnsemble   = "encadrement_est_ensemble"

	// eptOpenEndedPiece is the pièces bucket published as "N pièces et plus".
	eptOpenEndedPiece = 4

	// Zonage géographique (GeoJSON, un feature par commune portant sa zone).
	// Used to resolve a coordinate to its sub-communal rent-control zone.
	rawPlaineCommuneZonesName = "encadrement_plaine_commune_zones.raw.geojson"
	rawPlaineCommuneZonesURL  = "https://static.data.gouv.fr/resources/encadrement-des-loyers-de-plaine-commune/20220608-122433/quartier-plaine-commune-geodata.json"

	rawEstEnsembleZonesName = "encadrement_est_ensemble_zones.raw.geojson"
	rawEstEnsembleZonesURL  = "https://static.data.gouv.fr/resources/encadrement-des-loyers-de-est-ensemble/20220608-121232/quartier-est-ensemble-geodata.json"

	rawLyonName = "encadrement_lyon_villeurbanne.raw.geojson"
	rawLyonURL  = "https://download.data.grandlyon.com/wfs/grandlyon?SERVICE=WFS&VERSION=2.0.0&request=GetFeature&typename=metropole-de-lyon:car_care.carencadrmtloyer_2025_2026&outputFormat=application/json&SRSNAME=EPSG:4326"
)

// zoneCoordDecimals rounds zonage coordinates to ~11 cm — far finer than any
// rent-control boundary, and enough to keep the embedded geometry compact.
const zoneCoordDecimals = 6

// parisYear pins the published vintage kept from the Paris export, which
// carries every year since 2019. The committed snapshot keeps only the
// most recent grille (annee 2025). Bump when a newer arrêté lands.
const parisYear = 2025

// lyonOpenEndedPiece is the upstream label for the open-ended ("4 et
// plus") piece bucket in the Lyon "valeurs" object. The committed Lyon
// snapshot drops it (Lyon publishes 1/2/3 plus the open-ended cell; the
// snapshot keeps the three closed buckets only).
const lyonOpenEndedPiece = "4 et plus"

// transformParis rebuilds encadrement_paris.json from the opendata.paris.fr
// JSON export, keeping only parisYear and mapping the export fields to the
// committed parisRow shape (meuble_txt → bool, ref/min/max → *_eur_m2).
func transformParis(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawParisName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// parisExportRow is the upstream export shape (a superset of parisRow:
	// annee is a string, meuble is meuble_txt, and the geo columns are
	// dropped).
	type parisExportRow struct {
		Annee             string  `json:"annee"`
		IDZone            int     `json:"id_zone"`
		IDQuartier        int     `json:"id_quartier"`
		NomQuartier       string  `json:"nom_quartier"`
		CodeGrandQuartier int     `json:"code_grand_quartier"`
		Piece             int     `json:"piece"`
		Epoque            string  `json:"epoque"`
		MeubleTxt         string  `json:"meuble_txt"`
		Ref               float64 `json:"ref"`
		Min               float64 `json:"min"`
		Max               float64 `json:"max"`
	}

	var in []parisExportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&in); err != nil {
		return fmt.Errorf("encadrement: decode paris export: %w", err)
	}

	out := make([]parisRow, 0, len(in))
	for _, r := range in {
		y, err := strconv.Atoi(strings.TrimSpace(r.Annee))
		if err != nil || y != parisYear {
			continue
		}
		out = append(out, parisRow{
			Annee:             y,
			IDZone:            r.IDZone,
			IDQuartier:        r.IDQuartier,
			NomQuartier:       r.NomQuartier,
			CodeGrandQuartier: r.CodeGrandQuartier,
			Piece:             r.Piece,
			Epoque:            r.Epoque,
			Meuble:            isMeuble(r.MeubleTxt),
			RefEURPerM2:       r.Ref,
			MinEURPerM2:       r.Min,
			MaxEURPerM2:       r.Max,
		})
	}
	if len(out) == 0 {
		return fmt.Errorf("encadrement: paris transform produced no rows for annee %d", parisYear)
	}
	return json.NewEncoder(dst).Encode(out)
}

// eptEpoque pairs a DRIHL KML époque slug (used in the filename) with the
// construction-period label persisted in the barème artifact.
type eptEpoque struct{ slug, label string }

// The DRIHL barème axes. Their cartesian product is one KML file per cell;
// each file carries every zone as a Placemark. eptEpoques is kept in the
// order the committed artifact groups époques (older → newer).
var (
	eptEpoques = []eptEpoque{
		{"inf1946", "avant 1946"},
		{"1946-1970", "1946-1970"},
		{"1971-1990", "1971-1990"},
		{"sup1990", "apres 1990"},
	}
	eptPieces    = []int{1, 2, 3, 4}
	eptLogements = []struct {
		slug   string
		maison bool
	}{{"appartement", false}, {"maison", true}}
	eptMeubles = []struct {
		slug   string
		meuble bool
	}{{"non-meuble", false}, {"meuble", true}}
)

// eptKMLCombo is one point of the barème grid; it names the KML file that
// holds that cell for every zone.
type eptKMLCombo struct {
	logementSlug string
	maison       bool
	piece        int
	epoque       eptEpoque
	meubleSlug   string
	meuble       bool
}

// eptKMLCombos enumerates the grid in the committed artifact's row order
// (zone-major sorting is applied afterwards): logement → pièces → époque →
// meublé.
func eptKMLCombos() []eptKMLCombo {
	out := make([]eptKMLCombo, 0, len(eptLogements)*len(eptPieces)*len(eptEpoques)*len(eptMeubles))
	for _, lg := range eptLogements {
		for _, pc := range eptPieces {
			for _, ep := range eptEpoques {
				for _, mb := range eptMeubles {
					out = append(out, eptKMLCombo{
						logementSlug: lg.slug, maison: lg.maison,
						piece: pc, epoque: ep,
						meubleSlug: mb.slug, meuble: mb.meuble,
					})
				}
			}
		}
	}
	return out
}

// kmlBasename is the DRIHL filename for this cell.
func (c eptKMLCombo) kmlBasename() string {
	return fmt.Sprintf("drihl_medianes_%s_%d_%s_%s.kml", c.logementSlug, c.piece, c.epoque.slug, c.meubleSlug)
}

// eptRawFiles builds the Raw file list for one EPT: one entry per grid cell,
// its datadir basename prefixed with namePrefix (both EPTs share the datadir)
// and its URL under kmlBase (the DRIHL vintage directory).
func eptRawFiles(namePrefix, kmlBase string) []dataset.File {
	combos := eptKMLCombos()
	out := make([]dataset.File, 0, len(combos))
	for _, c := range combos {
		base := c.kmlBasename()
		out = append(out, dataset.File{
			Name: namePrefix + "_" + base,
			URL:  kmlBase + "/" + base,
		})
	}
	return out
}

// transformPlaineCommune / transformEstEnsemble rebuild the embedded EPT barème
// from the DRIHL référence-loyer KML files. Both EPTs publish the identical
// grid, so they share transformEPTBareme.
func transformPlaineCommune(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformEPTBareme(raw, eptRawNamePlaineCommune, dst)
}

func transformEstEnsemble(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformEPTBareme(raw, eptRawNameEstEnsemble, dst)
}

// eptKMLCell is one zone's rates within a single KML file.
type eptKMLCell struct {
	zone          int
	ref, min, max float64
}

func transformEPTBareme(raw dataset.RawSet, namePrefix string, dst io.Writer) error {
	var out []eptBaremeRow
	for _, c := range eptKMLCombos() {
		name := namePrefix + "_" + c.kmlBasename()
		cells, err := readEPTKML(raw, name)
		if err != nil {
			return fmt.Errorf("encadrement: %s: %w", name, err)
		}
		for _, cell := range cells {
			out = append(out, eptBaremeRow{
				Zone:           cell.zone,
				Piece:          c.piece,
				PieceOpenEnded: c.piece == eptOpenEndedPiece,
				Epoque:         c.epoque.label,
				Meuble:         c.meuble,
				Maison:         c.maison,
				RefEURPerM2:    cell.ref,
				MinEURPerM2:    cell.min,
				MaxEURPerM2:    cell.max,
			})
		}
	}
	if len(out) == 0 {
		return fmt.Errorf("encadrement: %s transform produced no rows", namePrefix)
	}
	// Deterministic, review-friendly order: zone-major, then the non-maison /
	// non-meublé apartment block first, iterating pièces then époques.
	epoqueIdx := map[string]int{}
	for i, e := range eptEpoques {
		epoqueIdx[e.label] = i
	}
	rank := func(r eptBaremeRow) [5]int {
		return [5]int{r.Zone, boolRank(r.Maison), boolRank(r.Meuble), r.Piece, epoqueIdx[r.Epoque]}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		for k := range ri {
			if ri[k] != rj[k] {
				return ri[k] < rj[k]
			}
		}
		return false
	})
	return json.NewEncoder(dst).Encode(out)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readEPTKML parses one DRIHL barème KML, returning one cell per distinct
// zone (a zone whose geometry is split across several Placemarks carries the
// same rates in each, so the first wins).
func readEPTKML(raw dataset.RawSet, name string) ([]eptKMLCell, error) {
	rc, err := raw.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	var doc struct {
		Placemarks []struct {
			Data []struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value"`
			} `xml:"ExtendedData>Data"`
		} `xml:"Document>Placemark"`
	}
	if err := xml.NewDecoder(dataset.BOMReader(rc)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode kml: %w", err)
	}

	seen := map[int]bool{}
	cells := make([]eptKMLCell, 0, len(doc.Placemarks))
	for _, pm := range doc.Placemarks {
		m := make(map[string]string, len(pm.Data))
		for _, d := range pm.Data {
			m[d.Name] = strings.TrimSpace(d.Value)
		}
		zs := m["idZone"]
		if zs == "" {
			continue
		}
		zone, err := strconv.Atoi(zs)
		if err != nil {
			return nil, fmt.Errorf("bad idZone %q: %w", zs, err)
		}
		if seen[zone] {
			continue
		}
		ref, ok1 := parseDotFloat(m["ref"])
		mn, ok2 := parseDotFloat(m["refmin"])
		mx, ok3 := parseDotFloat(m["refmaj"])
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("zone %d: bad rate cell (ref=%q min=%q max=%q)", zone, m["ref"], m["refmin"], m["refmaj"])
		}
		seen[zone] = true
		cells = append(cells, eptKMLCell{zone: zone, ref: ref, min: mn, max: mx})
	}
	if len(cells) == 0 {
		return nil, errors.New("no placemarks carrying an idZone")
	}
	return cells, nil
}

// parseDotFloat parses a dot-decimal rate (the KML uses "27.3"); a comma
// decimal is tolerated defensively.
func parseDotFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// zoneGeoSpec describes the per-EPT GeoJSON property keys used to extract a
// feature's zone identity; the two EPTs name these columns differently.
type zoneGeoSpec struct {
	ept        string
	rawName    string
	zoneKey    string
	inseeKey   string
	communeKey string
}

var (
	plaineCommuneZoneSpec = zoneGeoSpec{
		ept: ZoneSourcePlaineCommune, rawName: rawPlaineCommuneZonesName,
		zoneKey: "Zone", inseeKey: "INSEE_COM", communeKey: "NOM_COM",
	}
	estEnsembleZoneSpec = zoneGeoSpec{
		ept: ZoneSourceEstEnsemble, rawName: rawEstEnsembleZonesName,
		zoneKey: "Zone", inseeKey: "com_code", communeKey: "com_name",
	}
)

func transformPlaineCommuneZones(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformZones(raw, plaineCommuneZoneSpec, dst)
}

func transformEstEnsembleZones(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformZones(raw, estEnsembleZoneSpec, dst)
}

// transformZones compacts an EPT zonage GeoJSON into the embedded zone artifact:
// one zoneRow per feature carrying only (ept, zone, insee, commune) and the
// boundary geometry, with every other upstream property dropped and coordinates
// rounded to zoneCoordDecimals.
func transformZones(raw dataset.RawSet, spec zoneGeoSpec, dst io.Writer) error {
	rc, err := raw.Open(spec.rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	var fc struct {
		Features []struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&fc); err != nil {
		return fmt.Errorf("encadrement: decode %s: %w", spec.rawName, err)
	}

	out := make([]zoneRow, 0, len(fc.Features))
	for i, f := range fc.Features {
		zone := scalarString(f.Properties[spec.zoneKey])
		insee := scalarString(f.Properties[spec.inseeKey])
		commune := scalarString(f.Properties[spec.communeKey])
		if zone == "" || insee == "" {
			return fmt.Errorf("encadrement: %s feature %d: missing zone/insee (zone=%q insee=%q)", spec.rawName, i, zone, insee)
		}
		polys, err := geoindex.DecodeGeoJSONGeometry(f.Geometry.Type, f.Geometry.Coordinates, zoneCoordDecimals)
		if err != nil {
			return fmt.Errorf("encadrement: %s feature %d (%s): %w", spec.rawName, i, commune, err)
		}
		out = append(out, zoneRow{EPT: spec.ept, Zone: zone, INSEE: insee, Commune: commune, Polygons: polys})
	}
	if len(out) == 0 {
		return fmt.Errorf("encadrement: %s transform produced no zones", spec.rawName)
	}
	return json.NewEncoder(dst).Encode(out)
}

// scalarString coerces a JSON property value (string or number) to its trimmed
// textual form, returning "" for an absent or unparseable value.
func scalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s scalarText
	if err := s.UnmarshalJSON(raw); err != nil {
		return ""
	}
	return strings.TrimSpace(string(s))
}

// transformLyon rebuilds encadrement_lyon_villeurbanne.json from the
// data.grandlyon.com WFS GeoJSON. Each feature is one IRIS carrying a
// nested "valeurs" object keyed by piece → époque → meublé. The committed
// snapshot drops the open-ended ("4 et plus") piece bucket and flattens the
// rest into one row per (iris, piece, époque, meublé), preserving the
// upstream key order (hence the json.Decoder token walk rather than a map).
func transformLyon(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawLyonName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// lyonFeature keeps "valeurs" raw so we can walk its keys in upstream
	// order; the remaining columns decode normally.
	type lyonProps struct {
		CodeIRIS json.Number     `json:"codeiris"`
		Zonage   json.Number     `json:"zonage"`
		Commune  string          `json:"commune"`
		Insee    string          `json:"insee"`
		Valeurs  json.RawMessage `json:"valeurs"`
	}
	type lyonFeature struct {
		Properties lyonProps `json:"properties"`
	}
	type lyonFC struct {
		Features []lyonFeature `json:"features"`
	}

	var fc lyonFC
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&fc); err != nil {
		return fmt.Errorf("encadrement: decode lyon geojson: %w", err)
	}

	out := make([]lyonRow, 0, len(fc.Features)*30)
	for _, f := range fc.Features {
		p := f.Properties
		cells, err := flattenLyonValeurs(p.Valeurs)
		if err != nil {
			return fmt.Errorf("encadrement: lyon iris %s: %w", p.CodeIRIS.String(), err)
		}
		for _, c := range cells {
			mn, mx := c.minore, c.majore
			out = append(out, lyonRow{
				Insee:       p.Insee,
				IRIS:        p.CodeIRIS.String(),
				Zone:        p.Zonage.String(),
				Commune:     p.Commune,
				Piece:       c.piece,
				Epoque:      c.epoque,
				Meuble:      c.meuble,
				RefEURPerM2: c.reference,
				MinEURPerM2: &mn,
				MaxEURPerM2: &mx,
			})
		}
	}
	if len(out) == 0 {
		return errors.New("encadrement: lyon transform produced no rows")
	}
	return json.NewEncoder(dst).Encode(out)
}

// lyonCell is one flattened Lyon grid cell.
type lyonCell struct {
	piece     int
	epoque    string
	meuble    bool
	reference float64
	minore    float64
	majore    float64
}

// lyonRates is the leaf object of the Lyon "valeurs" tree (one per
// meublé / non-meublé under a piece × époque).
type lyonRates struct {
	Reference float64 `json:"loyer_reference"`
	Majore    float64 `json:"loyer_reference_majore"`
	Minore    float64 `json:"loyer_reference_minore"`
}

// flattenLyonValeurs walks the nested "valeurs" object (piece → époque →
// {"meuble"|"non meuble"} → rates) preserving the upstream key order, and
// drops the open-ended piece bucket. Order preservation is what makes the
// rebuilt array byte-identical to the committed snapshot, so we decode the
// object levels with json.Decoder token streams rather than maps.
func flattenLyonValeurs(rawValeurs json.RawMessage) ([]lyonCell, error) {
	var cells []lyonCell
	err := walkOrderedObject(rawValeurs, func(pieceKey string, pieceRaw json.RawMessage) error {
		if pieceKey == lyonOpenEndedPiece {
			return nil // committed snapshot omits the open-ended bucket
		}
		piece, err := strconv.Atoi(pieceKey)
		if err != nil {
			return fmt.Errorf("piece key %q: %w", pieceKey, err)
		}
		return walkOrderedObject(pieceRaw, func(epoque string, epoqueRaw json.RawMessage) error {
			return walkOrderedObject(epoqueRaw, func(meubleKey string, ratesRaw json.RawMessage) error {
				var rates lyonRates
				if err := json.Unmarshal(ratesRaw, &rates); err != nil {
					return fmt.Errorf("rates (%s/%s): %w", epoque, meubleKey, err)
				}
				cells = append(cells, lyonCell{
					piece:     piece,
					epoque:    epoque,
					meuble:    meubleKey == "meuble",
					reference: rates.Reference,
					minore:    rates.Minore,
					majore:    rates.Majore,
				})
				return nil
			})
		})
	})
	return cells, err
}

// walkOrderedObject calls fn for each key/value pair of a JSON object, in
// the order they appear in the input bytes.
func walkOrderedObject(raw json.RawMessage, fn func(key string, value json.RawMessage) error) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected JSON object, got %v", tok)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("expected object key, got %v", keyTok)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return err
		}
		if err := fn(key, val); err != nil {
			return err
		}
	}
	return nil
}

// scalarText decodes a JSON scalar (string or number) into its textual
// form, used for fields the upstream types loosely (Plaine Commune's
// nombre_de_piece is a number for 1/2/3 but a string for "4 et plus").
type scalarText string

func (s *scalarText) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) >= 2 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = scalarText(str)
		return nil
	}
	*s = scalarText(b)
	return nil
}

// isMeuble maps the Paris "meuble_txt" label ("meublé" / "non meublé") to a
// bool.
func isMeuble(txt string) bool {
	return strings.EqualFold(strings.TrimSpace(txt), "meublé")
}

// validateParis / validatePlaineCommune / validateLyon gate publication:
// the rebuilt artifact must parse as the expected row array and be
// non-empty.
func validateParis(r io.Reader) error {
	var rows []parisRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return fmt.Errorf("encadrement: validate paris: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("encadrement: validated paris artifact is empty")
	}
	return nil
}

func validatePlaineCommune(r io.Reader) error {
	return validateEPTBareme(r, "plaine commune")
}

func validateEstEnsemble(r io.Reader) error {
	return validateEPTBareme(r, "est ensemble")
}

func validateEPTBareme(r io.Reader, label string) error {
	var rows []eptBaremeRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return fmt.Errorf("encadrement: validate %s: %w", label, err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("encadrement: validated %s artifact is empty", label)
	}
	return nil
}

// validateZones gates a freshly-built zonage artifact: it must parse as a
// non-empty zoneRow array and every zone must carry an EPT, a zone id and at
// least one polygon.
func validateZones(r io.Reader) error {
	var rows []zoneRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return fmt.Errorf("encadrement: validate zones: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("encadrement: validated zones artifact is empty")
	}
	for _, z := range rows {
		if z.EPT == "" || z.Zone == "" || len(z.Polygons) == 0 {
			return fmt.Errorf("encadrement: zone %q/%q has no geometry", z.EPT, z.Zone)
		}
	}
	return nil
}

func validateLyon(r io.Reader) error {
	var rows []lyonRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return fmt.Errorf("encadrement: validate lyon: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("encadrement: validated lyon artifact is empty")
	}
	return nil
}

package encadrement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// Raw input filenames (datadir basenames) and upstream URLs --------------
//
// Each city is an independent dataset.Set with its own raw input and
// Transform. The committed JSON arrays are reproduced byte-equivalently
// from these upstreams (proven via the python diffs in the design notes):
//
//   - Paris: opendata.paris.fr JSON export, filtered to parisYear.
//   - Plaine Commune: a flat data.gouv.fr JSON array.
//   - Lyon/Villeurbanne: a data.grandlyon.com WFS GeoJSON whose per-IRIS
//     "valeurs" object nests piece × époque × meublé cells.
const (
	rawParisName = "encadrement_paris.raw.json"
	rawParisURL  = "https://opendata.paris.fr/api/explore/v2.1/catalog/datasets/logement-encadrement-des-loyers/exports/json"

	rawPlaineCommuneName = "encadrement_plaine_commune.raw.json"
	rawPlaineCommuneURL  = "https://static.data.gouv.fr/resources/encadrement-des-loyers-de-plaine-commune/20220608-122406/encadrements-plaine-commune.json"

	rawEstEnsembleName = "encadrement_est_ensemble.raw.json"
	rawEstEnsembleURL  = "https://static.data.gouv.fr/resources/encadrement-des-loyers-de-est-ensemble/20230601-202658/encadrements-est-ensemble-2023.json"

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

// pcOpenEndedPiece is the upstream label for the open-ended piece bucket
// in the Plaine Commune array (kept, mapped to Piece=4 / open-ended).
const pcOpenEndedPiece = "4 et plus"

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

// eptBaremeExportRow is the upstream array shape shared by the two
// Seine-Saint-Denis EPT barèmes (Plaine Commune, Est Ensemble): the same
// columns, French-formatted decimal strings (prix_min/med/max) and a
// "4 et plus" piece label. nombre_de_piece is a JSON number for the closed
// buckets (1/2/3) but the string "4 et plus" for the open-ended one, so it
// decodes through a permissive scalar.
type eptBaremeExportRow struct {
	Zone        int        `json:"zone"`
	NombrePiece scalarText `json:"nombre_de_piece"`
	Annee       string     `json:"annee_de_construction"`
	PrixMin     scalarText `json:"prix_min"`
	PrixMed     scalarText `json:"prix_med"`
	PrixMax     scalarText `json:"prix_max"`
	Maison      bool       `json:"maison"`
	Meuble      bool       `json:"meuble"`
}

// transformPlaineCommune / transformEstEnsemble rebuild the embedded EPT barème
// from the data.gouv.fr flat JSON array. Both EPTs publish the identical schema,
// so they share transformEPTBareme.
func transformPlaineCommune(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformEPTBareme(raw, rawPlaineCommuneName, dst)
}

func transformEstEnsemble(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	return transformEPTBareme(raw, rawEstEnsembleName, dst)
}

func transformEPTBareme(raw dataset.RawSet, rawName string, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	var in []eptBaremeExportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&in); err != nil {
		return fmt.Errorf("encadrement: decode %s: %w", rawName, err)
	}

	out := make([]eptBaremeRow, 0, len(in))
	for _, r := range in {
		piece, openEnded, err := parsePiece(string(r.NombrePiece), pcOpenEndedPiece)
		if err != nil {
			return fmt.Errorf("encadrement: %s piece %q: %w", rawName, r.NombrePiece, err)
		}
		ref, ok1 := parseFrenchFloat(string(r.PrixMed))
		mn, ok2 := parseFrenchFloat(string(r.PrixMin))
		mx, ok3 := parseFrenchFloat(string(r.PrixMax))
		if !ok1 || !ok2 || !ok3 {
			return fmt.Errorf("encadrement: %s zone %d: bad price cell (med=%q min=%q max=%q)", rawName, r.Zone, r.PrixMed, r.PrixMin, r.PrixMax)
		}
		out = append(out, eptBaremeRow{
			Zone:           r.Zone,
			Piece:          piece,
			PieceOpenEnded: openEnded,
			Epoque:         r.Annee,
			Meuble:         r.Meuble,
			Maison:         r.Maison,
			RefEURPerM2:    ref,
			MinEURPerM2:    mn,
			MaxEURPerM2:    mx,
		})
	}
	if len(out) == 0 {
		return fmt.Errorf("encadrement: %s transform produced no rows", rawName)
	}
	return json.NewEncoder(dst).Encode(out)
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
		polys, err := decodeGeometry(f.Geometry.Type, f.Geometry.Coordinates)
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

// decodeGeometry normalises a GeoJSON Polygon or MultiPolygon into the
// [polygon][ring][vertex][lon,lat] shape, rounding coordinates. Vertices may
// carry a third (altitude) ordinate upstream; only lon/lat are kept.
func decodeGeometry(typ string, coords json.RawMessage) ([][][][2]float64, error) {
	switch typ {
	case "Polygon":
		var p [][][]float64
		if err := json.Unmarshal(coords, &p); err != nil {
			return nil, fmt.Errorf("polygon coords: %w", err)
		}
		return [][][][2]float64{roundRings(p)}, nil
	case "MultiPolygon":
		var mp [][][][]float64
		if err := json.Unmarshal(coords, &mp); err != nil {
			return nil, fmt.Errorf("multipolygon coords: %w", err)
		}
		out := make([][][][2]float64, 0, len(mp))
		for _, p := range mp {
			out = append(out, roundRings(p))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type %q", typ)
	}
}

func roundRings(rings [][][]float64) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, ring := range rings {
		rr := make([][2]float64, 0, len(ring))
		for _, v := range ring {
			if len(v) < 2 {
				continue
			}
			rr = append(rr, [2]float64{roundTo(v[0], zoneCoordDecimals), roundTo(v[1], zoneCoordDecimals)})
		}
		out = append(out, rr)
	}
	return out
}

func roundTo(f float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(f*p) / p
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

// parsePiece maps a piece label to (piece, openEnded). The open-ended label
// ("4 et plus") becomes Piece=4 / openEnded=true; a bare integer becomes
// Piece=N / openEnded=false.
func parsePiece(label, openEndedLabel string) (int, bool, error) {
	if strings.EqualFold(strings.TrimSpace(label), openEndedLabel) {
		return 4, true, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(label))
	if err != nil {
		return 0, false, err
	}
	return n, false, nil
}

// parseFrenchFloat parses a French-formatted decimal ("16,4", "1 234,5")
// into a float64. ok is false for an empty/unparseable cell. Numeric JSON
// values that already arrived as plain numbers (no comma) parse too.
func parseFrenchFloat(s string) (float64, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', ' ', ' ':
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
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

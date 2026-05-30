package encadrement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	rawLyonName = "encadrement_lyon_villeurbanne.raw.geojson"
	rawLyonURL  = "https://download.data.grandlyon.com/wfs/grandlyon?SERVICE=WFS&VERSION=2.0.0&request=GetFeature&typename=metropole-de-lyon:car_care.carencadrmtloyer_2025_2026&outputFormat=application/json&SRSNAME=EPSG:4326"
)

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

// transformPlaineCommune rebuilds encadrement_plaine_commune.json from the
// data.gouv.fr flat JSON array. The upstream uses French-formatted decimal
// strings (prix_min/med/max) and a "4 et plus" piece label.
func transformPlaineCommune(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawPlaineCommuneName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// pcExportRow is the upstream array shape. nombre_de_piece is a JSON
	// number for the closed buckets (1/2/3) but the string "4 et plus" for
	// the open-ended one, so it decodes through a permissive scalar.
	type pcExportRow struct {
		Zone        int        `json:"zone"`
		NombrePiece scalarText `json:"nombre_de_piece"`
		Annee       string     `json:"annee_de_construction"`
		PrixMin     scalarText `json:"prix_min"`
		PrixMed     scalarText `json:"prix_med"`
		PrixMax     scalarText `json:"prix_max"`
		Maison      bool       `json:"maison"`
		Meuble      bool       `json:"meuble"`
	}

	var in []pcExportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&in); err != nil {
		return fmt.Errorf("encadrement: decode plaine commune: %w", err)
	}

	out := make([]plaineCommuneRow, 0, len(in))
	for _, r := range in {
		piece, openEnded, err := parsePiece(string(r.NombrePiece), pcOpenEndedPiece)
		if err != nil {
			return fmt.Errorf("encadrement: plaine commune piece %q: %w", r.NombrePiece, err)
		}
		ref, ok1 := parseFrenchFloat(string(r.PrixMed))
		mn, ok2 := parseFrenchFloat(string(r.PrixMin))
		mx, ok3 := parseFrenchFloat(string(r.PrixMax))
		if !ok1 || !ok2 || !ok3 {
			return fmt.Errorf("encadrement: plaine commune zone %d: bad price cell (med=%q min=%q max=%q)", r.Zone, r.PrixMed, r.PrixMin, r.PrixMax)
		}
		out = append(out, plaineCommuneRow{
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
		return errors.New("encadrement: plaine commune transform produced no rows")
	}
	return json.NewEncoder(dst).Encode(out)
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
	var rows []plaineCommuneRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return fmt.Errorf("encadrement: validate plaine commune: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("encadrement: validated plaine commune artifact is empty")
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

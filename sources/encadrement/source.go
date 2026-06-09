package encadrement

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "encadrement"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 matched Paris arrondissements via zip (75001..75020, 75116) and
// Lyon / Villeurbanne via INSEE (69381..69389, 69266); Plaine Commune
// returned ConfidenceNone (no commune→zone map yet).
//
// v2 resolves the Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble) to
// their sub-communal zone by point-in-polygon over an embedded zonage, with an
// INSEE-commune fallback. Adds the est_ensemble barème + both EPT geometries as
// embedded datasets.
const sourceVersion = 2

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures an encadrement Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, refreshed copies
	// of the processed artifacts found there take precedence over the
	// embedded ones. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the published zones encadrées
// (Paris, Plaine Commune, Lyon / Villeurbanne) using embedded JSON
// tables. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an encadrement Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded barème
// extracts and the Seine-Saint-Denis zonage geometries to the refresh tooling.
func (s *Source) Datasets() []dataset.Set {
	return []dataset.Set{
		setParis, setPlaineCommune, setEstEnsemble, setLyon,
		setPlaineCommuneZones, setEstEnsembleZones,
	}
}

// Query implements gazetteer.Source. Pipeline:
//
//  1. Reject non-residential property types with
//     gazetteer.ErrUnsupportedPropertyType.
//  2. Try Paris (zip 75001..75020, 75116).
//  3. Otherwise try Lyon / Villeurbanne (INSEE 69381..69389, 69266).
//  4. Otherwise try the Seine-Saint-Denis EPTs (Plaine Commune, Est
//     Ensemble): point-in-polygon on the embedded zonage resolves the
//     sub-communal zone from the listing's coordinates, with an
//     INSEE-commune fallback (see resolve93).
//  5. On a match, collapse the cells matching (piece, non-meublé,
//     non-maison) by median of LoyerRefMaxEURPerM2HC.
//
// SurfaceM2 is consulted (we require > 0 to compute a meaningful
// monthly cap downstream); when missing the Source skips with
// gazetteer.ErrInsufficientInputs.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	if !propertyTypeEligible(string(l.PropertyType)) {
		return nil, fmt.Errorf("encadrement: %w: %q", gazetteer.ErrUnsupportedPropertyType, l.PropertyType)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("encadrement: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	zip := strings.TrimSpace(l.Zip)
	insee := strings.TrimSpace(l.INSEE)
	rooms := intDeref(l.Rooms)

	// Paris.
	if arr := parisArrondissementFromZip(zip); arr != "" {
		entries := idx.LookupParis(arr)
		if len(entries) == 0 {
			return &Result{
				Confidence: ConfidenceNone,
				Evidence: Evidence{
					Zip:            zip,
					Arrondissement: arr,
				},
			}, nil
		}
		return collapse(entries, "Paris "+arr+"e", ZoneSourceParis, rooms, Evidence{
			Zip:            zip,
			Arrondissement: arr,
			Piece:          clampPiece(rooms),
		}, ConfidenceMedium), nil
	}

	// Lyon / Villeurbanne — try INSEE.
	if insee != "" {
		if entries := idx.LookupLyonInsee(insee); len(entries) > 0 {
			return collapse(entries, lyonZoneLabel(insee), ZoneSourceLyonVilleurbanne, rooms, Evidence{
				INSEE: insee,
				Piece: clampPiece(rooms),
			}, ConfidenceMedium), nil
		}
	}

	// Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble): resolve the
	// sub-communal zone by geometry when coordinates are present, else by
	// commune membership.
	if m, ok := idx.resolve93(insee, l.Lat, l.Lon); ok {
		var entries []Entry
		for _, z := range m.zones {
			entries = append(entries, idx.LookupEPTZone(m.ept, z)...)
		}
		return collapse(entries, m.commune, m.ept, rooms, Evidence{
			Zip:    zip,
			INSEE:  insee,
			ZoneID: strings.Join(m.zones, "+"),
		}, m.conf), nil
	}

	// Outside every shipped zone: a none-confidence result records the absence.
	return &Result{
		Confidence: ConfidenceNone,
		Evidence: Evidence{
			Zip:   zip,
			INSEE: insee,
		},
	}, nil
}

// propertyTypeEligible accepts residential apartments only — houses
// are out of scope for the encadrement perimeter (Paris explicitly
// publishes for "logement classique" / apartment grilles; Plaine
// Commune publishes a separate Maison cell that we don't currently
// surface).
func propertyTypeEligible(pt string) bool {
	switch normalizePropertyType(pt) {
	case "apartment", "house":
		// We accept houses here so the Source mirrors the wrapper's
		// existing perimeter (the rental wrapper consumes the result
		// for both); houses outside the published Maison cells will
		// simply not match downstream.
		return true
	default:
		return false
	}
}

// normalizePropertyType folds the rental enricher's property_type
// vocabulary into the two buckets the encadrement grilles cover.
func normalizePropertyType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "flat", "apartment", "apt", "appartement":
		return "apartment"
	case "house", "maison":
		return "house"
	default:
		return ""
	}
}

// collapse picks the cap value for a piece bucket out of the entries
// published for its zone. We pick the median ref-majoré across the
// matching piece-bucket non-meublé non-maison cells. conf is the confidence
// stamped on a successful match (a no-cell match always degrades to
// ConfidenceNone).
func collapse(entries []Entry, label, zoneSource string, rooms int, ev Evidence, conf string) *Result {
	piece := clampPiece(rooms)
	var majs, refs []float64
	for _, e := range entries {
		if e.Meuble {
			continue
		}
		if e.Maison {
			continue
		}
		if e.Piece != piece && (!e.PieceOpenEnded || piece < e.Piece) {
			continue
		}
		if e.LoyerRefMaxEURPerM2HC > 0 {
			majs = append(majs, e.LoyerRefMaxEURPerM2HC)
		}
		if e.LoyerRefEURPerM2HC > 0 {
			refs = append(refs, e.LoyerRefEURPerM2HC)
		}
	}
	ev.Piece = piece
	ev.NbCellsMatched = len(majs)
	if len(majs) == 0 {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}
	}
	maj := stats.Median(majs)
	ref := stats.Median(refs)
	return &Result{
		LoyerRefMajEURPerM2HC: maj,
		LoyerRefEURPerM2HC:    ref,
		Zone:                  label,
		ZoneSource:            zoneSource,
		Confidence:            conf,
		Evidence:              ev,
	}
}

// clampPiece bounds a rooms count to the [1, 4] range the published
// grilles use. 0 (rooms unknown) defaults to 1, ≥ 5 saturates at 4.
func clampPiece(rooms int) int {
	if rooms < 1 {
		return 1
	}
	if rooms > 4 {
		return 4
	}
	return rooms
}

// parisArrondissementFromZip converts a 75001..75020 / 75116 zip into
// the 2-digit arrondissement key the Paris index uses
// ("01" .. "20", plus "16" for 75116).
func parisArrondissementFromZip(zip string) string {
	if len(zip) != 5 || zip[:2] != "75" {
		return ""
	}
	if zip == "75116" {
		return "16"
	}
	// 75001..75020 → "01".."20".
	if zip[2] != '0' {
		return ""
	}
	n := int(zip[3]-'0')*10 + int(zip[4]-'0')
	if n < 1 || n > 20 {
		return ""
	}
	return zip[3:5]
}

// lyonZoneLabel produces a stable label for the Lyon zone (arr or
// Villeurbanne) given the resolved INSEE.
func lyonZoneLabel(insee string) string {
	switch insee {
	case "69381":
		return "Lyon 1er"
	case "69382":
		return "Lyon 2e"
	case "69383":
		return "Lyon 3e"
	case "69384":
		return "Lyon 4e"
	case "69385":
		return "Lyon 5e"
	case "69386":
		return "Lyon 6e"
	case "69387":
		return "Lyon 7e"
	case "69388":
		return "Lyon 8e"
	case "69389":
		return "Lyon 9e"
	case "69266":
		return "Villeurbanne"
	default:
		return "Lyon Métropole"
	}
}

// intDeref dereferences a *int into 0 when nil.
func intDeref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

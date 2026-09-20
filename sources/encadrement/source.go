package encadrement

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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
//
// v3 re-ingests the EPT barèmes from the authoritative DRIHL référence-loyer
// KML (arrêté "du 01 juin 2026") instead of the obsolete 2022/2023 data.gouv
// flat export — caps rise ~6-13% across the 93. Bumped so any datadir cache
// built from the stale v2 barème is superseded by the embedded v3 artifact.
//
// v4 fixes what the grille was read with. The Lyon snapshot now carries the
// "4 et plus" bucket it used to drop, so a Lyon or Villeurbanne flat of four
// rooms or more is no longer reported outside the perimeter; Listing.BuildYear
// selects the époque cell instead of the cap being a median across every
// construction period; an absent Listing.Rooms spans the grille at
// ConfidenceLow instead of silently quoting the studio cap; and Paris resolves
// from INSEE as well as zip (Lyon from zip as well as INSEE). Bumped so a
// datadir cache built from the v3 Lyon artifact is superseded.
const sourceVersion = 4

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
//  2. Try Paris (INSEE 75101..75120, or zip 75001..75020 / 75116 when the
//     listing carries no INSEE).
//  3. Otherwise try Lyon / Villeurbanne (INSEE 69381..69389 / 69266, or
//     zip 69001..69009 / 69100 likewise).
//  4. Otherwise try the Seine-Saint-Denis EPTs (Plaine Commune, Est
//     Ensemble): point-in-polygon on the embedded zonage resolves the
//     sub-communal zone from the listing's coordinates, with an
//     INSEE-commune fallback (see resolve93).
//  5. On a match, collapse the cells matching (pièces, époque,
//     non-meublé, non-maison) by median of LoyerRefMaxEURPerM2HC.
//
// Rooms and BuildYear both narrow the grille cell, and both are optional:
// an absent one spans every published bucket for that axis and is recorded
// in the Evidence (an absent Rooms additionally caps the Confidence at
// ConfidenceLow, because the cap per m² falls by a fifth to a third from a
// studio to a four-room flat). SurfaceM2 is not consulted: this Source
// publishes a per-m² cap, and multiplying by a surface is the caller's step.
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
	sel := cellFilter{
		piece:     clampPiece(intDeref(l.Rooms)),
		buildYear: usableBuildYear(l.BuildYear, time.Now()),
	}

	// Paris. Either identifier resolves the arrondissement, INSEE first: a
	// hand-built Listing routinely carries one without the other, and treating
	// an INSEE-only Paris address as unregulated is the worst possible answer.
	if arr := parisArrondissement5(zip, insee); arr != "" {
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
		return collapse(entries, "Paris "+arr+"e", ZoneSourceParis, sel, Evidence{
			Zip:            zip,
			INSEE:          insee,
			Arrondissement: arr,
		}, ConfidenceMedium), nil
	}

	// Lyon / Villeurbanne, keyed by INSEE with a zip fallback.
	if lyon := lyonINSEE(insee, zip); lyon != "" {
		if entries := idx.LookupLyonInsee(lyon); len(entries) > 0 {
			return collapse(entries, lyonZoneLabel(lyon), ZoneSourceLyonVilleurbanne, sel, Evidence{
				Zip:   zip,
				INSEE: lyon,
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
		return collapse(entries, m.commune, m.ept, sel, Evidence{
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

// cellFilter names the grille cells a query selects inside its zone.
//
// Both axes are optional and a zero means "unknown, span every published
// bucket" — the grille has no cell for "any number of rooms", so spanning is
// the only honest reading of a missing input, and the median across the
// spanned cells is what the Result then carries.
type cellFilter struct {
	// piece is the rooms bucket (1..4), or 0 when Rooms was absent.
	piece int
	// buildYear is the construction year, or 0 when BuildYear was absent
	// or implausible (see usableBuildYear).
	buildYear int
}

// matches reports whether e is one of the cells this filter selects. Furnished
// and maison cells are never selected: the published meublé grille is a
// different cap, and the rare maison cells are a different perimeter.
func (f cellFilter) matches(e Entry) bool {
	if e.Meuble || e.Maison {
		return false
	}
	if f.piece > 0 && e.Piece != f.piece && (!e.PieceOpenEnded || f.piece < e.Piece) {
		return false
	}
	if f.buildYear > 0 && !epoqueCovers(e.Epoque, f.buildYear) {
		return false
	}
	return true
}

// collapse picks the cap out of the cells published for a zone: the median
// loyer de référence majoré (and, in parallel, the median loyer de référence)
// across the cells sel selects. conf is the confidence stamped on a successful
// match; a no-cell match always degrades to ConfidenceNone, and an unknown
// rooms count caps it at ConfidenceLow.
//
// When a BuildYear selects no cell — a territory whose époque vocabulary this
// package does not parse — the collapse retries across every époque rather
// than reporting the address unregulated, and says so in the Evidence.
func collapse(entries []Entry, label, zoneSource string, sel cellFilter, ev Evidence, conf string) *Result {
	majs, refs, epoque := selectCells(entries, sel)
	if len(majs) == 0 && sel.buildYear > 0 {
		sel.buildYear = 0
		majs, refs, epoque = selectCells(entries, sel)
		ev.EpoqueUnmatched = true
	}

	ev.Piece = sel.piece
	ev.BuildYear = sel.buildYear
	ev.Epoque = epoque
	ev.NbCellsMatched = len(majs)
	if len(majs) == 0 {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}
	}
	if sel.piece == 0 && conf == ConfidenceMedium {
		conf = ConfidenceLow
	}
	return &Result{
		LoyerRefMajEURPerM2HC: stats.Median(majs),
		LoyerRefEURPerM2HC:    stats.Median(refs),
		Zone:                  label,
		ZoneSource:            zoneSource,
		Confidence:            conf,
		Evidence:              ev,
	}
}

// selectCells gathers the majoré and référence readings of the cells sel
// selects. epoque is the single époque label behind them, or "" when the
// selection spans several (an unknown BuildYear, or a zone whose cells
// disagree).
func selectCells(entries []Entry, sel cellFilter) (majs, refs []float64, epoque string) {
	spans := false
	for _, e := range entries {
		if !sel.matches(e) {
			continue
		}
		switch {
		case spans:
		case epoque == "":
			epoque = e.Epoque
		case epoque != e.Epoque:
			spans, epoque = true, ""
		}
		if e.LoyerRefMaxEURPerM2HC > 0 {
			majs = append(majs, e.LoyerRefMaxEURPerM2HC)
		}
		if e.LoyerRefEURPerM2HC > 0 {
			refs = append(refs, e.LoyerRefEURPerM2HC)
		}
	}
	return majs, refs, epoque
}

// clampPiece bounds a rooms count to the [1, 4] range the published grilles
// use, saturating at the open-ended top bucket ("4 pièces et plus" in Paris
// and the 93, "4 et plus" in Lyon). A count below 1 means "rooms unknown" and
// maps to 0, which cellFilter reads as "span every bucket": defaulting an
// unknown count to a studio instead would quote the single most expensive cap
// of the grille (38.00 against 29.45 EUR/m²/month for Paris 1er) as if it had
// been looked up.
func clampPiece(rooms int) int {
	if rooms < 1 {
		return 0
	}
	if rooms > 4 {
		return 4
	}
	return rooms
}

// parisArrondissement5 extracts the 2-digit Paris arrondissement key
// ("01" .. "20") from a listing's identifiers. Empty when neither names a
// Paris arrondissement.
//
// The INSEE is the authoritative commune code and wins whenever it is set:
// the zip is only consulted for a listing that carries none. A listing whose
// INSEE says Saint-Denis and whose zip says 75001 is therefore resolved as
// Saint-Denis, not silently promoted to Paris on the weaker identifier — the
// same rule lyonINSEE follows.
func parisArrondissement5(zip, insee string) string {
	if insee != "" {
		return parisArrondissementFromINSEE(insee)
	}
	return parisArrondissementFromZip(zip)
}

// parisArrondissementFromZip converts a 75001..75020 / 75116 zip into
// the 2-digit arrondissement key the Paris index uses
// ("01" .. "20", plus "16" for 75116).
func parisArrondissementFromZip(zip string) string {
	if zip == "75116" {
		return "16"
	}
	return twoDigitKey(arrondissementNumber(zip, "750", 20))
}

// parisArrondissementFromINSEE converts a 75101..75120 commune code — what the
// BAN returns for any Paris address — into the same 2-digit key. The parent
// code 75056 carries no arrondissement and yields "".
func parisArrondissementFromINSEE(insee string) string {
	return twoDigitKey(arrondissementNumber(insee, "751", 20))
}

// twoDigitKey zero-pads an arrondissement number into the index's key, or
// returns "" for the 0 that arrondissementNumber uses for "not one".
func twoDigitKey(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%02d", n)
}

// lyonINSEE resolves a listing to a Métropole de Lyon commune code inside the
// encadrement perimeter: the nine Lyon arrondissements (69381..69389) and
// Villeurbanne (69266), by INSEE or, when the listing carries none, by zip.
// The zip fallback is not a general zip→INSEE table, only the unambiguous
// perimeter one: 69001..69009 are the Lyon arrondissements and 69100 is
// Villeurbanne. Empty for anything else.
func lyonINSEE(insee, zip string) string {
	if insee != "" {
		if insee == "69266" || arrondissementNumber(insee, "6938", 9) > 0 {
			return insee
		}
		// A set INSEE is the authoritative commune code: it names some other
		// commune, so the zip must not promote the listing into the Lyon
		// perimeter behind its back.
		return ""
	}
	if zip == "69100" {
		return "69266"
	}
	// 69001..69009 → 69381..69389.
	if n := arrondissementNumber(zip, "6900", 9); n > 0 {
		return fmt.Sprintf("6938%d", n)
	}
	return ""
}

// arrondissementNumber reads the arrondissement number off a 5-character code
// that starts with prefix, bounded by count. It returns 0 when code is not
// such a code — a different commune, a malformed length, a non-digit tail, or
// an out-of-range number like the "00" of a parent code.
//
// The prefix carries the spelling, which differs per family: Paris
// arrondissements are "751" + a 2-digit number, Lyon's are "6938" + a single
// digit, and their zips are "750"/"6900" + the same.
func arrondissementNumber(code, prefix string, count int) int {
	if len(code) != 5 || !strings.HasPrefix(code, prefix) {
		return 0
	}
	n, err := strconv.Atoi(code[len(prefix):])
	if err != nil || n < 1 || n > count {
		return 0
	}
	return n
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

// QueryResult is Query with the package's typed Result — for callers
// holding a constructed Source instance. Equivalent to the package-level
// Query helper without rebuilding the Source per call.
func (s *Source) QueryResult(ctx context.Context, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, s, l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

package encadrement

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
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
//
// v5 reads the zone the cap is actually set on. Paris caps are per quartier
// and Lyon's per IRIS, but both used to collapse a whole arrondissement at
// ConfidenceMedium: a Paris 13e, 3-room, 1971-1990 flat in quartier Gare was
// told 27.15 EUR/m2/month where its own cell says 19.80. A Listing.IRIS of
// the arrondissement now pins the quartier (Paris) or the IRIS (Lyon), and a
// collapse across distinct caps is ConfidenceLow, the rule the multi-zone 93
// communes already followed.
//
// v6 places a Seine-Saint-Denis listing by point-in-polygon only at street
// precision or finer: a commune centre used to pin the mairie's zone of a
// multi-zone commune at ConfidenceMedium, and now takes the commune path.
//
// v7 ships the Paris grille in force since 1 July 2026 (arrêté of 12 June
// 2026). v6 still served the 2025 one, 3.3 % lower on average and up to
// 8.5 % lower on a cell. Bumped so a datadir copy built from the 2025 export
// is superseded by the embedded 2026 snapshot.
//
// v8 reads a house in Plaine Commune or Est Ensemble from the maison grille
// those EPTs publish (Evidence.Maison). v7 dropped every maison cell and
// served the apartment one, about 16 % lower in zone 311.
//
// v9 also returns the FURNISHED cap (LoyerRefMajMeubleEURPerM2HC,
// LoyerRefMeubleEURPerM2HC), collapsed from the meublé cells every shipped
// grille publishes beside the unfurnished ones. v8 dropped every meublé cell,
// so a furnished letting could only be held to the unfurnished ceiling, which
// is lower on every cell (Paris 1er, 1 room, pre-1946: 42.60 against 48.60
// EUR/m2/month).
//
// v10 tells a decreed rent-cap territory without a shipped grille from an
// unregulated commune. Lille, Bordeaux, Montpellier, the Pays basque and
// Grenoble-Alpes Métropole cap rents, yet v9 answered for them exactly as for
// Toulouse (empty, ConfidenceNone), which reads as "no cap". They now carry
// Regulation (RegulationNotCovered, or RegulationPartialCommune for the
// Grenoble communes covered in part), Territory and an Evidence.Note, at
// ConfidenceNone and with no cap figure; IsEmpty is false for them.
//
// v11 honours the date (Listing.AsOf, or today). Past 24 November 2026, the
// last day of the art. 140 experiment, with no extension in the JO, every
// regulated reading is RegulationLapsed with no cap; a date outside a shipped
// grille's own arrêté period lowers the confidence (before it) or withdraws
// the figure (after it). v10 quoted the 2026 caps for any date.
const sourceVersion = 11

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
//     non-meublé, maison for a house where the zone publishes a maison
//     grille, apartment otherwise) by median of LoyerRefMaxEURPerM2HC, and
//     the meublé cells of the same selection into the furnished pair.
//
// Rooms and BuildYear both narrow the grille cell, and both are optional:
// an absent one spans every published bucket for that axis and is recorded
// in the Evidence (an absent Rooms additionally caps the Confidence at
// ConfidenceLow, because the cap per m² falls by a fifth to a third from a
// studio to a four-room flat). SurfaceM2 is not consulted: this Source
// publishes a per-m² cap, and multiplying by a surface is the caller's step.
//
// The reading is then held to its date, Listing.AsOf or today (see dated in
// legal.go): past the end of the art. 140 experiment no cap is law
// (RegulationLapsed), and a date outside the shipped grille's own period
// degrades or withdraws its figure.
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

	asOf := l.AsOf
	if asOf.IsZero() {
		asOf = clock()
	}
	r := lookup(idx, l)
	return dated(r, r.ZoneSource, dayOf(asOf), elanLastDay), nil
}

// lookup resolves the listing's zone and collapses its cells, with no regard
// to the date (Query applies dated on top).
func lookup(idx *Index, l gazetteer.Listing) *Result {
	zip := strings.TrimSpace(l.Zip)
	insee := strings.TrimSpace(l.INSEE)
	sel := cellFilter{
		piece:     clampPiece(intDeref(l.Rooms)),
		buildYear: usableBuildYear(l.BuildYear, time.Now()),
		house:     normalizePropertyType(string(l.PropertyType)) == "house",
	}

	// Paris. Either identifier resolves the arrondissement, INSEE first: a
	// hand-built Listing routinely carries one without the other, and treating
	// an INSEE-only Paris address as unregulated is the worst possible answer.
	if arr := parisArrondissement5(zip, insee); arr != "" {
		entries := idx.LookupParis(arr)
		if len(entries) == 0 {
			return &Result{
				Confidence: ConfidenceNone,
				Regulation: RegulationNotCovered,
				Territory:  "Paris",
				Evidence: Evidence{
					Zip:            zip,
					Arrondissement: arr,
					Note:           "Paris is capped (décret n° 2019-315) but the embedded grille has no cell for arrondissement " + arr,
				},
			}
		}
		ev := Evidence{
			Zip:            zip,
			INSEE:          insee,
			Arrondissement: arr,
		}
		// Paris caps are set per quartier, and an arrondissement can straddle
		// up to four secteurs (the 13e reads 19.80 to 28.70 EUR/m2/month on
		// one cell). The IRIS carries the quartier: pin it when it does, and
		// otherwise mark a multi-secteur collapse as the ambiguity it is.
		conf := ConfidenceMedium
		if q := parisQuartierZone(l.IRIS, arr); q != "" {
			if sub := entriesInZone(entries, q); len(sub) > 0 {
				entries, ev.ZoneID = sub, q
			}
		}
		if ev.ZoneID == "" && spansSeveralCaps(entries) {
			conf = ConfidenceLow
		}
		return collapse(entries, "Paris "+arr+"e", ZoneSourceParis, sel, ev, conf)
	}

	// Lyon / Villeurbanne, keyed by INSEE with a zip fallback.
	if lyon := lyonINSEE(insee, zip); lyon != "" {
		if entries := idx.LookupLyonInsee(lyon); len(entries) > 0 {
			ev := Evidence{Zip: zip, INSEE: lyon}
			// The Lyon grille is zoned per IRIS (five zones; Lyon 3e reads
			// 12.50 to 16.30 EUR/m2/month on one cell). A caller-supplied
			// IRIS of this commune pins its own cells; without one, a
			// collapse across distinct caps is Low, as in Paris.
			conf := ConfidenceMedium
			if iris := strings.TrimSpace(l.IRIS); len(iris) == 9 && strings.HasPrefix(iris, lyon) {
				if sub := idx.LookupLyonIRIS(iris); len(sub) > 0 {
					entries, ev.ZoneID = sub, iris
				}
			}
			if ev.ZoneID == "" && spansSeveralCaps(entries) {
				conf = ConfidenceLow
			}
			return collapse(entries, lyonZoneLabel(lyon), ZoneSourceLyonVilleurbanne, sel, ev, conf)
		}
	}

	// Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble): resolve the
	// sub-communal zone by geometry when coordinates are present at street
	// precision or finer, else by commune membership. A commune centre lies
	// in SOME zone of a multi-zone commune (the mairie's), so it must take
	// the commune path, not pin that zone as the address's.
	var zLat, zLon *float64
	if lat, lon, ok := l.CoordsAtLeast(banx.PrecisionStreet); ok {
		zLat, zLon = &lat, &lon
	}
	if m, ok := idx.resolve93(insee, zLat, zLon); ok {
		var entries []Entry
		for _, z := range m.zones {
			entries = append(entries, idx.LookupEPTZone(m.ept, z)...)
		}
		return collapse(entries, m.commune, m.ept, sel, Evidence{
			Zip:    zip,
			INSEE:  insee,
			ZoneID: strings.Join(m.zones, "+"),
		}, m.conf)
	}

	// A decreed territory whose grille is not shipped: the rent IS capped, by
	// a figure this Source does not carry. Reporting it like the commune next
	// door would tell the caller there is no cap at all.
	if t, partial, ok := lookupTerritory(insee); ok {
		return uncoveredResult(t, partial, Evidence{Zip: zip, INSEE: insee})
	}

	// Outside every decreed territory: a none-confidence result records the
	// absence.
	return &Result{
		Confidence: ConfidenceNone,
		Evidence: Evidence{
			Zip:   zip,
			INSEE: insee,
		},
	}
}

// uncoveredResult is the reading for a commune of a decreed rent-cap territory
// whose grille the Source does not ship: no cap figure, ConfidenceNone, and a
// Regulation that says the rent is (or, for a partial commune, may be) capped.
func uncoveredResult(t territory, partial bool, ev Evidence) *Result {
	reg := RegulationNotCovered
	ev.Note = fmt.Sprintf("%s is a rent-cap territory (%s, art. 140 loi 2018-1021); its grille is not embedded, so the cap is unknown, not absent", t.name, t.decree)
	if partial {
		reg = RegulationPartialCommune
		ev.Note = fmt.Sprintf("part of this commune (a set of IRIS listed in the annex of the %s) is a rent-cap territory of %s; the perimeter and its grille are not embedded, so the address may be capped", t.decree, t.name)
	}
	return &Result{
		Confidence: ConfidenceNone,
		Regulation: reg,
		Territory:  t.name,
		Evidence:   ev,
	}
}

// propertyTypeEligible accepts the two residential types the grilles cover.
// The encadrement applies to every dwelling let as a main residence, house or
// apartment: Paris and Lyon set one grille for both, and the two 93 EPTs
// publish a separate maison grille, which collapse selects for a house.
func propertyTypeEligible(pt string) bool {
	switch normalizePropertyType(pt) {
	case "apartment", "house":
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
	// house is true for a house listing. It reads the zone's maison cells
	// where the zone publishes them (collapse sets maison then), and the
	// shared grille where it does not.
	house bool
	// maison selects the maison cells instead of the apartment ones.
	maison bool
	// meuble selects the meublé (furnished) cells instead of the unfurnished
	// ones. collapse runs the selection once each way.
	meuble bool
}

// matches reports whether e is one of the cells this filter selects. The
// meublé and unfurnished cells are two different caps, told apart by
// f.meuble, and the maison and apartment cells are told apart by f.maison:
// the two 93 EPTs publish a separate maison grille, which in Plaine Commune
// zone 311 sits about 16 % above the apartment one.
func (f cellFilter) matches(e Entry) bool {
	if e.Meuble != f.meuble || e.Maison != f.maison {
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
//
// The unfurnished selection decides the reading (its époque fallback, its
// emptiness, its confidence); the furnished pair is then the same selection
// over the meublé cells, so the two caps always describe the same cells.
func collapse(entries []Entry, label, zoneSource string, sel cellFilter, ev Evidence, conf string) *Result {
	if sel.house {
		for _, e := range entries {
			if e.Maison {
				sel.maison = true
				ev.Maison = true
				break
			}
		}
	}
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
		// Inside a shipped zone, so the rent is capped; the grille simply has
		// no cell for this selection.
		ev.Note = "the address lies in a shipped rent-cap zone but no grille cell matched the selection"
		return &Result{
			Confidence: ConfidenceNone,
			Regulation: RegulationNotCovered,
			Territory:  label,
			Evidence:   ev,
		}
	}
	if sel.piece == 0 && conf == ConfidenceMedium {
		conf = ConfidenceLow
	}
	r := &Result{
		LoyerRefMajEURPerM2HC: stats.Median(majs),
		LoyerRefEURPerM2HC:    stats.Median(refs),
		Zone:                  label,
		ZoneSource:            zoneSource,
		Confidence:            conf,
		Regulation:            RegulationCapped,
	}
	furnished := sel
	furnished.meuble = true
	if mMajs, mRefs, _ := selectCells(entries, furnished); len(mMajs) > 0 {
		r.LoyerRefMajMeubleEURPerM2HC = stats.Median(mMajs)
		r.LoyerRefMeubleEURPerM2HC = stats.Median(mRefs)
		ev.NbMeubleCellsMatched = len(mMajs)
	}
	r.Evidence = ev
	return r
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

// parisQuartierZone reads the quartier off a Paris IRIS code and returns it as
// the barème's zone identifier (code_grand_quartier), or "" when the IRIS
// names no quartier of arrondissement arr.
//
// A Paris IRIS is "751" + the arrondissement (2 digits) + the quartier's
// city-wide number (2 digits, 01..80) + a sequence (2 digits): "751135001" is
// the first IRIS of quartier 50, Gare, in the 13e. The IRIS are nested in the
// quartiers, so its first seven characters ARE the code_grand_quartier the
// Ville de Paris keys its grille on ("7511350"). An IRIS of another
// arrondissement is refused rather than trusted over the INSEE.
func parisQuartierZone(iris, arr string) string {
	iris = strings.TrimSpace(iris)
	if len(iris) != 9 || !strings.HasPrefix(iris, "751"+arr) {
		return ""
	}
	q, err := strconv.Atoi(iris[5:7])
	if err != nil || q < 1 || q > 80 {
		return ""
	}
	if _, err := strconv.Atoi(iris[7:]); err != nil {
		return ""
	}
	return iris[:7]
}

// entriesInZone keeps the cells of one zone (a Paris quartier).
func entriesInZone(entries []Entry, zoneID string) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.ZoneID == zoneID {
			out = append(out, e)
		}
	}
	return out
}

// spansSeveralCaps reports whether entries hold more than one zone with
// DIFFERENT caps on the same grille cell, i.e. whether collapsing them across
// zones blends distinct legal ceilings. An arrondissement inside a single
// secteur (the Paris 3e) does not; one straddling several (the 13e) does.
func spansSeveralCaps(entries []Entry) bool {
	type cell struct {
		piece  int
		epoque string
		meuble bool
		maison bool
	}
	seen := make(map[cell]float64, len(entries))
	for _, e := range entries {
		k := cell{e.Piece, e.Epoque, e.Meuble, e.Maison}
		if v, ok := seen[k]; ok && v != e.LoyerRefMaxEURPerM2HC {
			return true
		}
		seen[k] = e.LoyerRefMaxEURPerM2HC
	}
	return false
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

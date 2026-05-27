// Package proptype owns the single source of truth that maps raw
// property-type strings (any case, any language, possibly with trailing
// whitespace) onto the canonical enum stored in `auctions.property_type`.
//
// # Why this package exists
//
// Before its introduction, the idiom `strings.ToLower(strings.TrimSpace(pt))`
// followed by an ad-hoc switch over French/English aliases ("apt", "maison",
// "TERRAIN", "garage", "local commercial"…) was duplicated across at least
// twelve call sites: every enricher (MeilleursAgents, PappersImmo, DVF,
// BienIci, LocService), the matcher / consolidator, the
// estimation engine, and the auctionview score helper. Each site spelled
// the alias set slightly differently, so the same raw input could be
// classified as `apartment` in one place and `unknown` in another — a
// silent-leak class.
//
// The wire format is preserved. `auctions.property_type` is a `text`
// column whose stored values may NOT change ; this package only
// CONSOLIDATES the read-side normalisation. Callers writing the column
// should still emit the canonical English-ish slug (`apartment`,
// `house`, …) that this package's `String()` returns.
//
// # Canonical set
//
// The canonical values match exactly what the `extract.PropertyType`
// enum and the LLM-extract dictionary already write into the column :
//
//	apartment    — flat, studio, loft, "appartement"
//	house        — maison, villa, pavillon, "propriété"
//	land         — terrain, parcelle
//	parking      — place de parking, box, garage  (note: "garage" maps
//	               to parking here ; the legacy column may carry the
//	               literal "garage" string for older rows, which this
//	               package treats as a synonym of parking)
//	commercial   — local commercial, fonds de commerce, boutique
//	mixed        — ensemble immobilier, immeuble, "habitation + commerce"
//	parts        — share transfer (SCI/SAS), parts sociales
//	other        — annex spaces (cave, débarras, remise, grenier)
//	garage       — kept as a distinct canonical for legacy rows ; new
//	               classification should map "garage" onto parking
//	cave         — same legacy-row rationale as garage
//	""           — Unknown : the empty string sentinel ; not detected /
//	               not classifiable. Callers use this as a wildcard.
//
// `garage` and `cave` are kept as distinct canonicals (rather than
// folded into `parking` / `other`) because the doctor's `roomsIncompatible`
// / `dpeIncompatible` guards already enumerate them as separate values
// and folding would silently re-enable rooms/DPE writes on those rows.
// New code should prefer `parking` over `garage` ; the alias table maps
// "garage" → Garage (NOT Parking) to keep prod data stable.
package proptype

import "strings"

// PropertyType is the canonical enum value stored in auctions.property_type.
//
// Distinct from `a downstream consumer` (the extractor's
// internal enum) — this type carries the read-side canonical strings
// every consumer compares against.
type PropertyType string

// Canonical property-type values. The empty string Unknown denotes a
// non-detected / non-classifiable input — callers typically treat it as
// a wildcard ("don't filter on this field").
const (
	Apartment  PropertyType = "apartment"
	House      PropertyType = "house"
	Land       PropertyType = "land"
	Parking    PropertyType = "parking"
	Commercial PropertyType = "commercial"
	Mixed      PropertyType = "mixed"
	Parts      PropertyType = "parts"
	Garage     PropertyType = "garage"
	Cave       PropertyType = "cave"
	Other      PropertyType = "other"
	Unknown    PropertyType = ""
)

// String returns the underlying canonical slug ("apartment", "house", …).
func (p PropertyType) String() string { return string(p) }

// IsKnown reports whether p is one of the named canonical values (not
// Unknown). Exported for callers that want to distinguish a parsed-but-
// unsupported input from an absent one — though in practice Normalize
// already collapses unknown inputs to Unknown.
func (p PropertyType) IsKnown() bool {
	switch p {
	case Apartment, House, Land, Parking, Commercial, Mixed, Parts, Garage, Cave, Other:
		return true
	default:
		return false
	}
}

// aliases is the single source of truth that maps a lowercased+trimmed
// raw input onto the canonical PropertyType. Lookup is exact-match on
// the already-normalised key, so we keep the table small and explicit.
//
// Sources consulted when seeding this table (so future contributors
// understand which spellings each call site contributed):
//
//   - a downstream web layer::typeSynonyms (the broadest existing
//     alias map, French + abbreviations)
//   - a sibling module::classifyTitle
//   - a downstream consumer::propertyTypePatterns
//   - a downstream loader::canonicalPropertyTypes
//   - per-enricher MapPropertyType* helpers (MA, DVF, PappersImmo, BienIci,
//     LocService) which each accepted FR + EN spellings
//
// Keep alphabetical within each canonical bucket so drift on review is
// easy to spot.
var aliases = map[string]PropertyType{
	// --- Apartment ----------------------------------------------------------
	"apartment":   Apartment,
	"appart":      Apartment,
	"appartement": Apartment,
	"appt":        Apartment,
	"apt":         Apartment,
	"flat":        Apartment,
	"loft":        Apartment,
	"studette":    Apartment,
	"studio":      Apartment,

	// --- House --------------------------------------------------------------
	"house":    House,
	"maison":   House,
	"pavillon": House,
	"villa":    House,

	// --- Land ---------------------------------------------------------------
	"land":      Land,
	"land_only": Land,
	"parcelle":  Land,
	"terrain":   Land,

	// --- Parking ------------------------------------------------------------
	// Note: "garage" / "box" / "cave" map to their own canonicals (Garage,
	// Cave) to preserve legacy auctions.property_type rows. Use the Garage
	// and Cave canonicals when reading those rows ; new classifications
	// from current scrapers emit "parking" for vehicle storage.
	"parking": Parking,
	// "place de parking" is two tokens — Normalize sees it as a single
	// trimmed/lowered string. Map the full phrase plus the common short
	// forms.
	"place de parking": Parking,

	// --- Commercial ---------------------------------------------------------
	"boutique":         Commercial,
	"bureau":           Commercial,
	"commercial":       Commercial,
	"local":            Commercial,
	"local commercial": Commercial,

	// --- Mixed --------------------------------------------------------------
	"mixed": Mixed,
	"mixte": Mixed,

	// --- Parts (share transfer) --------------------------------------------
	"entity_sale": Parts,
	"part":        Parts,
	"parts":       Parts,
	"shares":      Parts,

	// --- Garage (legacy canonical, distinct from Parking) ------------------
	"box":    Garage,
	"garage": Garage,

	// --- Cave (legacy canonical, distinct from Other) ----------------------
	"cave": Cave,

	// --- Other --------------------------------------------------------------
	"other": Other,
}

// Normalize maps a raw input string onto a canonical PropertyType.
//
// Returns Unknown for the empty/whitespace-only input or when no alias
// matches. The function is case-insensitive and tolerates leading /
// trailing whitespace.
//
// Examples :
//
//	Normalize("Appartement")       -> Apartment
//	Normalize("maison ")           -> House
//	Normalize("TERRAIN")           -> Land
//	Normalize("garage")            -> Garage   (legacy canonical)
//	Normalize("local commercial")  -> Commercial
//	Normalize("")                  -> Unknown
//	Normalize("hovercraft")        -> Unknown
func Normalize(raw string) PropertyType {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return Unknown
	}
	if v, ok := aliases[key]; ok {
		return v
	}
	return Unknown
}

// NormalizePtr is a nil-tolerant variant of Normalize : returns Unknown
// when the pointer is nil, otherwise dereferences and normalises. Lets
// callers forward `*store.Auction.PropertyType` directly.
func NormalizePtr(raw *string) PropertyType {
	if raw == nil {
		return Unknown
	}
	return Normalize(*raw)
}

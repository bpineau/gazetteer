package proptype

import "strings"

// PropertyType is the canonical enum value Sources compare against when
// gating per-Source eligibility (see gazetteer.PropertyType). Carried
// as a typed string so it round-trips through JSON, log records and
// map keys without conversion.
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
// The table seeds from FR + EN spellings observed in real-estate
// listings, classified-ad titles and DPE / DVF / IGN feeds.
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
	"commerce":         Commercial,
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

package fraddr

import (
	"strings"

	"github.com/bpineau/gazetteer/helpers/frnorm"
)

// streetTypeCanon maps every spelling of a "type de voie" this package
// recognises, accent-folded and lower-cased, to one canonical form. It is
// wider than streetTypeTokens (the tokens Parse drops from a pattern):
// telling two homonymous streets apart needs every type BAN writes, even
// those Parse keeps in its tokens.
var streetTypeCanon = map[string]string{
	"rue": "rue", "r": "rue",
	"avenue": "avenue", "av": "avenue", "ave": "avenue",
	"boulevard": "boulevard", "bd": "boulevard", "bld": "boulevard", "boul": "boulevard",
	"chemin": "chemin", "ch": "chemin", "chem": "chemin",
	"impasse": "impasse", "imp": "impasse",
	"place": "place", "pl": "place",
	"allee": "allee", "all": "allee",
	"quai": "quai", "qu": "quai",
	"square": "square", "sq": "square",
	"route": "route", "rte": "route",
	"voie":    "voie",
	"passage": "passage", "pass": "passage",
	"villa": "villa",
	"cours": "cours", "crs": "cours",
	"cour":       "cour",
	"sentier":    "sentier",
	"sente":      "sente",
	"chaussee":   "chaussee",
	"esplanade":  "esplanade",
	"parvis":     "parvis",
	"rond-point": "rond-point", "rpt": "rond-point",
	"hameau": "hameau", "ham": "hameau",
	"lieu-dit": "lieu-dit", "ld": "lieu-dit",
	"cite":     "cite",
	"faubourg": "faubourg", "fbg": "faubourg",
	"residence": "residence", "res": "residence",
	"promenade": "promenade", "prom": "promenade",
	"mail":      "mail",
	"ruelle":    "ruelle",
	"traverse":  "traverse",
	"carrefour": "carrefour",
}

// numberSuffixTokens may sit between a house number and the street type:
// repetition suffixes ("12 bis rue X") and range connectors ("12 à 18 rue
// X", "5 et 7 rue X"). StreetOf skips them, with any digit run they bring.
var numberSuffixTokens = map[string]bool{
	"bis": true, "ter": true, "quater": true,
	"a": true, "et": true,
}

// Street is the identity of a street as written in an address: its
// canonical type de voie and its name, both accent-folded and
// lower-cased, with the house number, its suffix, the postcode and the
// city removed. Two spellings of one street share a Street ("8 av. de la
// République", "8 Avenue de la Republique 75011 Paris" both read
// {avenue, "de la republique"}); "rue Voltaire" and "boulevard Voltaire"
// do not.
type Street struct {
	// Type is the canonical type de voie ("rue", "avenue", "boulevard",
	// ...), or empty when the address names none this package knows.
	Type string
	// Name is the rest of the street label, its words single-spaced.
	// "St" and "Ste" are expanded to "saint" and "sainte".
	Name string
}

// IsZero reports whether no street could be read.
func (s Street) IsZero() bool { return s.Type == "" && s.Name == "" }

// String renders the street as "type name".
func (s Street) String() string {
	return strings.TrimSpace(s.Type + " " + s.Name)
}

// SameStreet reports whether a and b may name the same street, spelling
// aside: their types do not contradict each other (equal, or one of them
// unknown) and one name is a word-wise prefix of the other, which absorbs a
// city left after a postcode-less address ("rue x paris" against "rue x").
// It is a compatibility test, not an equality: a caller holding several
// compatible candidates must prefer an exact match (a == b) and treat
// distinct compatible streets as ambiguous.
func SameStreet(a, b Street) bool {
	if a.Name == "" || b.Name == "" {
		return false
	}
	if a.Type != "" && b.Type != "" && a.Type != b.Type {
		return false
	}
	return wordPrefix(a.Name, b.Name) || wordPrefix(b.Name, a.Name)
}

// wordPrefix reports whether the words of p start the words of s.
func wordPrefix(p, s string) bool {
	return s == p || strings.HasPrefix(s, p+" ")
}

// StreetOf reads the Street of a free-text French address. It applies
// Parse's anchoring rules (a residence-name prefix before the number is
// skipped) and keeps every word of the name, where Parse caps them at
// three. An address without a recognisable street yields the zero Street.
//
//	"106 Boulevard Voltaire 75011 Paris"   → {boulevard, "voltaire"}
//	"12 bis av. St-Michel"                 → {avenue, "saint michel"}
//	"8 Cour des Petites-Écuries"           → {cour, "des petites ecuries"}
func StreetOf(addr string) Street {
	addr = anchor(strings.TrimSpace(addr))
	addr = strings.ReplaceAll(addr, ",", " ")
	words := strings.Fields(frnorm.StripAccents(strings.ToLower(addr)))
	// Drop everything from the postcode on: it and the city that follows
	// are not part of the street.
	for i, w := range words {
		if isFrPostalCode(w) {
			words = words[:i]
			break
		}
	}
	// The house number, then its suffix or range tail.
	if len(words) > 0 && extractLeadingNumber(words[0]) != "" {
		words = words[1:]
		for len(words) > 0 && (numberSuffixTokens[words[0]] || isDigits(words[0])) {
			words = words[1:]
		}
	}
	var st Street
	if len(words) > 0 {
		if canon, ok := streetTypeCanon[strings.TrimRight(words[0], ".")]; ok {
			st.Type = canon
			words = words[1:]
		}
	}
	name := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".")
		for part := range strings.FieldsFuncSeq(w, func(r rune) bool { return r == '-' || r == '\'' || r == '’' }) {
			switch part {
			case "st":
				part = "saint"
			case "ste":
				part = "sainte"
			}
			name = append(name, part)
		}
	}
	st.Name = strings.Join(name, " ")
	return st
}

// anchor applies Parse's steps 1 and 1.5: an address that does not start
// with a digit is re-anchored on its first digit-led comma segment, else on
// an embedded "<n> <street-type>" run.
func anchor(addr string) string {
	if addr == "" || (addr[0] >= '0' && addr[0] <= '9') {
		return addr
	}
	for seg := range strings.SplitSeq(addr, ",") {
		seg = strings.TrimSpace(seg)
		if seg != "" && seg[0] >= '0' && seg[0] <= '9' {
			return seg
		}
	}
	if loc := embeddedHouseNumberRe.FindStringIndex(addr); loc != nil {
		return strings.TrimSpace(addr[loc[0]:])
	}
	return addr
}

// isDigits reports whether s is a nonempty ASCII digit run.
func isDigits(s string) bool {
	return s != "" && extractLeadingNumber(s) == s
}

package frnorm

import "strings"

// StripAccents folds Latin-1 / Latin-Extended-A diacritics onto their
// base ASCII letter. The input case is preserved (A → A, a → a). Unknown
// runes — emoji, punctuation, CJK, anything not in the fold table — are
// preserved as-is.
//
// Convention pinned by this package :
//
//   - Case-preserving (lawyer/avoventes parity ; the vench slug helper
//     lowercases first BEFORE calling — that's a slug concern, not an
//     accent-folding concern).
//   - "oe" → "oe", "OE" → "OE", "ae" → "ae", "AE" → "AE" (digraph expansion).
//   - "c" → "c", "C" → "C".
//   - Apostrophes (', ’, `) and quotes are PRESERVED. Vench's slug
//     helper drops them via its own dedicated rule ; general callers
//     (lawyer / avoventes substring matching) need them preserved so
//     "l'Etoile" doesn't collapse into "lEtoile" silently.
//   - Unknown runes (CJK, emoji, raw HTML like &nbsp;) are PRESERVED ;
//     callers that want a strict-ASCII output should pair this with a
//     follow-up filter pass.
//
// This function does NOT use golang.org/x/text/unicode/norm to keep the
// hot path allocation-free and dependency-light, and to match the
// existing avoventes / lawyer behaviour byte-for-byte.
func StripAccents(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Drop NFD combining marks (U+0300..U+036F) so a decomposed
		// "e + COMBINING ACUTE" folds to the same "e" as the precomposed
		// "é". Without this guard, NFD-encoded inputs would survive the
		// fold pass with stray combining bytes attached — silently
		// diverging from NFC outputs in downstream comparisons.
		if r >= 0x0300 && r <= 0x036F {
			continue
		}
		switch r {
		// a / A
		case 'à', 'á', 'â', 'ã', 'ä', 'å':
			b.WriteRune('a')
		case 'À', 'Á', 'Â', 'Ã', 'Ä', 'Å':
			b.WriteRune('A')
		// c / C
		case 'ç':
			b.WriteRune('c')
		case 'Ç':
			b.WriteRune('C')
		// e / E
		case 'è', 'é', 'ê', 'ë':
			b.WriteRune('e')
		case 'È', 'É', 'Ê', 'Ë':
			b.WriteRune('E')
		// i / I
		case 'ì', 'í', 'î', 'ï':
			b.WriteRune('i')
		case 'Ì', 'Í', 'Î', 'Ï':
			b.WriteRune('I')
		// n / N
		case 'ñ':
			b.WriteRune('n')
		case 'Ñ':
			b.WriteRune('N')
		// o / O
		case 'ò', 'ó', 'ô', 'õ', 'ö':
			b.WriteRune('o')
		case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö':
			b.WriteRune('O')
		// u / U
		case 'ù', 'ú', 'û', 'ü':
			b.WriteRune('u')
		case 'Ù', 'Ú', 'Û', 'Ü':
			b.WriteRune('U')
		// y / Y
		case 'ý', 'ÿ':
			b.WriteRune('y')
		case 'Ý', 'Ÿ':
			b.WriteRune('Y')
		// Digraphs.
		case 'œ':
			b.WriteString("oe")
		case 'Œ':
			b.WriteString("OE")
		case 'æ':
			b.WriteString("ae")
		case 'Æ':
			b.WriteString("AE")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

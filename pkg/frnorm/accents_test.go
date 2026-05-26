package frnorm

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// TestStripAccents locks the canonical case-preserving fold rules. The
// rules diverge from vench's pre-existing slug-only stripAccents (which
// drops apostrophes and unknown runes) — that's intentional ; vench
// keeps its own slug helper (Slugify in vench/documents.go) on top of
// this primitive.
func TestStripAccents(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Trivial passthrough.
		{"empty", "", ""},
		{"plain ascii", "Paris 75011", "Paris 75011"},

		// Single letters, lower.
		{"a-grave", "à", "a"},
		{"a-acute", "á", "a"},
		{"a-circ", "â", "a"},
		{"a-trema", "ä", "a"},
		{"e-acute", "é", "e"},
		{"e-grave", "è", "e"},
		{"e-circ", "ê", "e"},
		{"e-trema", "ë", "e"},
		{"i-circ", "î", "i"},
		{"o-circ", "ô", "o"},
		{"u-grave", "ù", "u"},
		{"c-cedilla", "ç", "c"},

		// Single letters, upper — case is preserved.
		{"A-acute", "Á", "A"},
		{"E-acute", "É", "E"},
		{"C-cedilla", "Ç", "C"},

		// Digraphs.
		{"oe lower", "œ", "oe"},
		{"oe upper", "Œ", "OE"},
		{"ae lower", "æ", "ae"},
		{"ae upper", "Æ", "AE"},

		// French specifics.
		{"e-acute word", "Évreux", "Evreux"},
		{"a-grave word", "à PARIS", "a PARIS"},
		{"l'Etoile preserved apostrophe", "l'Étoile", "l'Etoile"},
		{"curly apostrophe preserved", "l’Étoile", "l’Etoile"},
		{"backtick preserved", "l`Étoile", "l`Etoile"},
		{"oe in cœur", "cœur", "coeur"},
		{"oe in CŒUR", "CŒUR", "COEUR"},

		// Mixed real-world auction strings.
		{"address line", "Saint-Étienne, 42000", "Saint-Etienne, 42000"},
		{"vendu pour", "VENDU pour 6 000 €", "VENDU pour 6 000 €"},
		{"degree symbol preserved", "20°C", "20°C"},
		{"superscript 2 preserved", "73 m²", "73 m²"},

		// Unknown runes pass through untouched.
		{"emoji", "Paris ", "Paris "},
		{"CJK", "東京", "東京"},
		{"raw NBSP preserved", "à Paris", "a Paris"},

		// Idempotence.
		{"already stripped", "Evreux", "Evreux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StripAccents(c.in)
			if got != c.want {
				t.Errorf("StripAccents(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestStripAccentsIdempotent — applying the function twice must produce
// the same result as applying it once. Cheap property check.
func TestStripAccentsIdempotent(t *testing.T) {
	inputs := []string{
		"",
		"Paris",
		"Évreux",
		"l'Étoile",
		"cœur de Paris",
		"Saint-Étienne, 42000",
	}
	for _, s := range inputs {
		once := StripAccents(s)
		twice := StripAccents(once)
		if once != twice {
			t.Errorf("not idempotent for %q: once=%q twice=%q", s, once, twice)
		}
	}
}

// TestStripAccents_NFD_EqualsNFC asserts that decomposed (NFD) inputs
// produce the same ASCII output as precomposed (NFC) inputs. Before the
// combining-mark drop guard, "é" written as "e + U+0301" (NFD) kept the
// stray combining mark in the output, while "é" as U+00E9 (NFC) folded
// cleanly to "e" — two distinct strings for the same character. Both
// must now collapse to the same ASCII letter.
func TestStripAccents_NFD_EqualsNFC(t *testing.T) {
	cases := []struct {
		name string
		nfc  string
		nfd  string
		want string
	}{
		{"eacute lower", "é", "é", "e"},
		{"eacute upper", "É", "É", "E"},
		{"egrave lower", "è", "è", "e"},
		{"ecirc lower", "ê", "ê", "e"},
		{"agrave lower", "à", "à", "a"},
		{"ccedil lower", "ç", "ç", "c"},
		{"city NFD", "Évreux", "Évreux", "Evreux"},
		// Stray combining mark on plain ASCII — just drop the mark.
		{"bare combining", "abc", "ábc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNFC := StripAccents(tc.nfc)
			gotNFD := StripAccents(tc.nfd)
			if gotNFC != tc.want {
				t.Errorf("NFC StripAccents(%q) = %q, want %q", tc.nfc, gotNFC, tc.want)
			}
			if gotNFD != tc.want {
				t.Errorf("NFD StripAccents(%q) = %q, want %q", tc.nfd, gotNFD, tc.want)
			}
			if gotNFC != gotNFD {
				t.Errorf("NFC vs NFD divergence: %q != %q", gotNFC, gotNFD)
			}
		})
	}
}

// TestStripAccents_PropertyIdempotent — randomised idempotence check
// over UTF-8 strings. Catches future regressions where a new accent
// rule maps onto a target rune that itself has accents in the input
// table (chained folds break idempotence).
func TestStripAccents_PropertyIdempotent(t *testing.T) {
	prop := func(s string) bool {
		if !utf8.ValidString(s) {
			return true // skip invalid UTF-8 inputs (testing/quick generates them)
		}
		once := StripAccents(s)
		return once == StripAccents(once)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

// TestStripAccents_PropertyLengthBounded — output rune count must be
// ≤ input rune count + (count of digraph chars in input). Digraphs
// expand to 2 ASCII bytes; nothing else grows. Guards against a
// future accident that maps a single rune to 3+ bytes.
func TestStripAccents_PropertyLengthBounded(t *testing.T) {
	prop := func(s string) bool {
		if !utf8.ValidString(s) {
			return true
		}
		digraphs := strings.Count(s, "œ") + strings.Count(s, "Œ") +
			strings.Count(s, "æ") + strings.Count(s, "Æ")
		inRunes := utf8.RuneCountInString(s)
		outRunes := utf8.RuneCountInString(StripAccents(s))
		// Drop of combining marks may also reduce length — we only
		// upper-bound here.
		return outRunes <= inRunes+digraphs
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

// TestStripAccents_PropertyAllASCIIPreserved — strings made of pure
// ASCII letters/digits/punctuation must round-trip unchanged.
func TestStripAccents_PropertyAllASCIIPreserved(t *testing.T) {
	prop := func(s string) bool {
		for _, r := range s {
			if r > 127 {
				return true // skip non-ASCII
			}
		}
		return StripAccents(s) == s
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

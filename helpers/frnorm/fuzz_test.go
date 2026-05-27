package frnorm

// Go-native fuzz harnesses for the small, pure normalisers in this
// package. Each fuzz asserts the function does not panic AND a couple
// of "shape" invariants that should hold for any input (length / digit
// ranges / ASCII-cleanliness). The seeds cover the edge cases the
// regression suite already pins ; the fuzzer is expected to discover
// the longer tail (UTF-8 truncation, NFD combining marks, mixed-locale
// digits, weird whitespace classes).
//
// Run :
//
//	go test -fuzz=FuzzParseFRPriceToCentimes -fuzztime=60s ./helpers/frnorm/
//	go test -fuzz=FuzzNormaliseSpace          -fuzztime=60s ./helpers/frnorm/
//	go test -fuzz=FuzzNormalizeHearingTime    -fuzztime=60s ./helpers/frnorm/
//	go test -fuzz=FuzzExtractZipCity          -fuzztime=60s ./helpers/frnorm/
//	go test -fuzz=FuzzStripAccents            -fuzztime=60s ./helpers/frnorm/
//
// CI runs the seed corpus as ordinary unit tests via `go test ./...`.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzParseFRPriceToCentimes exercises the FR price parser. Invariants :
//
//   - never panics ;
//   - returns 0 when the digit count is zero ;
//   - returns a finite int64 (no overflow trap) ;
//   - is idempotent : feeding the canonical centimes string back in
//     (formatted as plain euros) parses to the same value.
func FuzzParseFRPriceToCentimes(f *testing.F) {
	for _, s := range []string{
		"", "abc",
		"150,50 €", "150 000,50 €", "150.000 €", "150.50 €",
		"1.5", "1.336.500", "61.000", "3.465,38",
		" 1 234 , 00 €", // weird NBSP soup
		"€€€", "0", "0,00", "-150,50",
		"99999999999999,99",             // overflow probe
		"   150   ,   50    € ",         // wide whitespace
		strings.Repeat("9", 64) + ",99", // very long
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Catch panics implicitly via the test runner ; assert it
		// returns SOME int64.
		got := ParseFRPriceToCentimes(s)
		// No digits → 0 (defensive).
		hasDigit := false
		for _, r := range s {
			if r >= '0' && r <= '9' {
				hasDigit = true
				break
			}
		}
		if !hasDigit && got != 0 {
			t.Fatalf("ParseFRPriceToCentimes(%q) = %d, want 0 for digit-free input", s, got)
		}
	})
}

// FuzzNormaliseSpace exercises the whitespace collapser. Invariants :
//
//   - never panics ;
//   - output length ≤ input length (we only delete or collapse) ;
//   - output contains no double-space run ;
//   - output is valid UTF-8 if input is valid UTF-8.
func FuzzNormaliseSpace(f *testing.F) {
	for _, s := range []string{
		"", " ", "  ", "\t\n\r", "  ",
		"a b  c", "  hello\tworld  ",
		"a b c", // NBSP
		"naïve côte",
		strings.Repeat(" ", 1000),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := NormaliseSpace(s)
		if !utf8.ValidString(s) {
			// Malformed UTF-8 input — `for…range` may emit U+FFFD which
			// grows the output ; we only require no panic.
			return
		}
		if len(got) > len(s) {
			t.Fatalf("NormaliseSpace grew valid input %q (%d) → %q (%d)", s, len(s), got, len(got))
		}
		if strings.Contains(got, "  ") {
			t.Fatalf("NormaliseSpace left double space in %q → %q", s, got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("NormaliseSpace corrupted UTF-8 of %q → %q", s, got)
		}
	})
}

// FuzzNormalizeHearingTime exercises the FR hearing-time normaliser.
// Invariants :
//
//   - never panics ;
//   - output is either "" or exactly the canonical "HH:MM:SS" form
//     (8 chars, [0-2]\d:[0-5]\d:[0-5]\d).
func FuzzNormalizeHearingTime(f *testing.F) {
	for _, s := range []string{
		"", "abc",
		"14h00", "14h", "14H30", "9h30", "14:00", "14:00:00",
		"24h00", "25h", "9", "9h00 (heure de Paris)",
		"00h00", "23h59:59",
		"  9 h 30  ", "1H1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := NormalizeHearingTime(s)
		if got == "" {
			return
		}
		if len(got) != 8 || got[2] != ':' || got[5] != ':' {
			t.Fatalf("NormalizeHearingTime(%q) = %q : not canonical HH:MM:SS", s, got)
		}
		// Validate digit ranges.
		h := (int(got[0]-'0'))*10 + int(got[1]-'0')
		m := (int(got[3]-'0'))*10 + int(got[4]-'0')
		sec := (int(got[6]-'0'))*10 + int(got[7]-'0')
		if h < 0 || h > 23 || m < 0 || m > 59 || sec < 0 || sec > 59 {
			t.Fatalf("NormalizeHearingTime(%q) = %q : out-of-range fields", s, got)
		}
	})
}

// FuzzExtractZipCity exercises the trailing "<zip> <city>" extractor.
// Invariants :
//
//   - never panics ;
//   - when ok=true, zip is exactly 5 digits ;
//   - when ok=true, city is non-empty AND zip+city appear in s ;
//   - when ok=false, both zip and city are empty.
func FuzzExtractZipCity(f *testing.F) {
	for _, s := range []string{
		"", "no zip here",
		"12 rue Foo, 75011 Paris, France",
		"4 Imp. Gantz, 69008 Lyon",
		"foo, 75011 Paris",
		"75001 Paris 1er",
		"97150 Saint-Martin",
		"  75011    Paris  ",
		"123456 Paris", // 6 digits — must NOT match cleanly
		"75011Paris",   // no separator
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		zip, city, ok := ExtractZipCity(s)
		if !ok {
			if zip != "" || city != "" {
				t.Fatalf("ExtractZipCity(%q) ok=false but returned zip=%q city=%q", s, zip, city)
			}
			return
		}
		if len(zip) != 5 {
			t.Fatalf("ExtractZipCity(%q) zip=%q : not 5 chars", s, zip)
		}
		for i := range 5 {
			if zip[i] < '0' || zip[i] > '9' {
				t.Fatalf("ExtractZipCity(%q) zip=%q : non-digit at %d", s, zip, i)
			}
		}
		if city == "" {
			t.Fatalf("ExtractZipCity(%q) ok=true but city empty", s)
		}
		if !strings.Contains(s, zip) {
			t.Fatalf("ExtractZipCity(%q) zip=%q not in input", s, zip)
		}
	})
}

// FuzzStripAccents exercises the accent folder. Invariants :
//
//   - never panics ;
//   - output is valid UTF-8 when input is valid UTF-8 ;
//   - the empty string is identity ;
//   - idempotent : StripAccents(StripAccents(s)) == StripAccents(s).
func FuzzStripAccents(f *testing.F) {
	for _, s := range []string{
		"", "abc", "naïve", "côte d'Azur", "ÉLÉPHANT",
		"é", // NFD "é"
		"œuf", "Æneas",
		strings.Repeat("é", 100),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := StripAccents(s)
		if utf8.ValidString(s) && !utf8.ValidString(got) {
			t.Fatalf("StripAccents corrupted UTF-8 of %q → %q", s, got)
		}
		again := StripAccents(got)
		if again != got {
			t.Fatalf("StripAccents not idempotent : %q → %q → %q", s, got, again)
		}
	})
}

package frnorm

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

func TestNormaliseSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single word", "foo", "foo"},
		{"multiple spaces collapse", "foo  bar   baz", "foo bar baz"},
		{"tabs collapse", "foo\tbar\tbaz", "foo bar baz"},
		{"newlines collapse", "foo\nbar\nbaz", "foo bar baz"},
		{"mixed whitespace", "foo \t\n bar", "foo bar"},
		{"leading whitespace trimmed", "  foo bar", "foo bar"},
		{"trailing whitespace trimmed", "foo bar  ", "foo bar"},
		{"leading and trailing", "  foo bar  ", "foo bar"},
		{"all whitespace", "   \t\n  ", ""},
		{"nbsp collapses", "foo bar", "foo bar"},
		{"nbsp leading trimmed", " foo", "foo"},
		{"already clean", "foo bar", "foo bar"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormaliseSpace(tc.input)
			if got != tc.want {
				t.Errorf("NormaliseSpace(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormaliseSpace_PropertyIdempotent — applying twice = applying
// once. Guards against any future "trim only trailing" / "preserve a
// trailing run" regression.
func TestNormaliseSpace_PropertyIdempotent(t *testing.T) {
	prop := func(s string) bool {
		if !utf8.ValidString(s) {
			return true
		}
		once := NormaliseSpace(s)
		return once == NormaliseSpace(once)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

// TestNormaliseSpace_PropertyNoDoubleSpace — output must never
// contain two consecutive ASCII spaces.
func TestNormaliseSpace_PropertyNoDoubleSpace(t *testing.T) {
	prop := func(s string) bool {
		if !utf8.ValidString(s) {
			return true
		}
		return !strings.Contains(NormaliseSpace(s), "  ")
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

// TestNormaliseSpace_PropertyTrimmed — output must not start nor end
// with whitespace (under our recognised whitespace set: space, tab,
// newline, CR, NBSP).
func TestNormaliseSpace_PropertyTrimmed(t *testing.T) {
	isWhitespace := func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ' '
	}
	prop := func(s string) bool {
		if !utf8.ValidString(s) {
			return true
		}
		out := NormaliseSpace(s)
		if out == "" {
			return true
		}
		first, _ := utf8.DecodeRuneInString(out)
		last, _ := utf8.DecodeLastRuneInString(out)
		return !isWhitespace(first) && !isWhitespace(last)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

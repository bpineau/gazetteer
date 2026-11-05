package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

// TestParseInterleaved_Terminator pins that "--" ends option parsing
// wherever it sits: what follows is positional verbatim, flag-looking or
// not, and a "--" given as a flag's value is that value.
func TestParseInterleaved_Terminator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv        []string
		wantPos     []string
		wantSurface float64
		wantName    string
	}{
		{[]string{"--", "fixture-address", "--surface", "99"}, []string{"fixture-address", "--surface", "99"}, 0, ""},
		{[]string{"a", "--", "-b"}, []string{"a", "-b"}, 0, ""},
		{[]string{"--surface", "12", "a", "--", "--surface", "99"}, []string{"a", "--surface", "99"}, 12, ""},
		{[]string{"a", "--surface", "46", "b"}, []string{"a", "b"}, 46, ""},
		{[]string{"--name", "--", "a", "--surface", "5"}, []string{"a"}, 5, "--"},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		surface := fs.Float64("surface", 0, "")
		name := fs.String("name", "", "")
		_ = fs.Bool("json", false, "")
		pos, err := parseInterleaved(fs, c.argv)
		if err != nil {
			t.Fatalf("%q: %v", c.argv, err)
		}
		if !reflect.DeepEqual(pos, c.wantPos) || *surface != c.wantSurface || *name != c.wantName {
			t.Errorf("%q: positional %q surface %v name %q; want %q %v %q", c.argv, pos, *surface, *name, c.wantPos, c.wantSurface, c.wantName)
		}
	}
}

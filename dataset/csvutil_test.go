package dataset

import (
	"io"
	"strings"
	"testing"
)

func TestBOMReader(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"with BOM", bom + "INSEE;val\n75056;1", "INSEE;val\n75056;1"},
		{"no BOM", "INSEE;val\n75056;1", "INSEE;val\n75056;1"},
		{"empty", "", ""},
		// Fewer than 3 bytes: Peek(3) errors, so nothing is stripped.
		{"short two bytes", "\xEF\xBB", "\xEF\xBB"},
		{"single byte", "x", "x"},
		// First two BOM bytes but wrong third: not a BOM, leave intact.
		{"partial-BOM lookalike", "\xEF\xBB\x41rest", "\xEF\xBB\x41rest"},
		// A BOM mid-stream must NOT be stripped (only a leading one).
		{"BOM not leading", "a" + bom + "b", "a" + bom + "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := io.ReadAll(BOMReader(strings.NewReader(tc.in)))
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("BOMReader(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

package proptype

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestToListingType(t *testing.T) {
	cases := []struct {
		in   string
		want gazetteer.PropertyType
		ok   bool
	}{
		{"appartement", gazetteer.PropertyApartment, true},
		{"Studio", gazetteer.PropertyApartment, true},
		{"villa", gazetteer.PropertyHouse, true},
		{"terrain", gazetteer.PropertyLand, true},
		{"local commercial", gazetteer.PropertyCommercial, true},
		// Canonical types with no Listing equivalent are gated out, not
		// mapped to Unknown-and-run-anyway.
		{"parking", gazetteer.PropertyUnknown, false},
		{"gibberish", gazetteer.PropertyUnknown, false},
		{"", gazetteer.PropertyUnknown, false},
	}
	for _, c := range cases {
		got, ok := ToListingType(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ToListingType(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

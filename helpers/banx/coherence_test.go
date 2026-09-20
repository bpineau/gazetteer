package banx

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/bpineau/gazetteer/helpers/communes"
)

// TestValidateCoherence_StraddlingCommunes pins the 23 communes whose
// postal code sits in another département than their INSEE. Every one of
// them is a CORRECT BAN answer, and the départemental-prefix comparison
// that used to answer this question called all 23 incoherent: the result
// was never cached and a warning was logged on every single lookup.
//
// The pairs are read from the embedded commune table rather than spelled
// out, so the guard is tested against the data it actually ships with.
func TestValidateCoherence_StraddlingCommunes(t *testing.T) {
	pairs := straddlingPairs(t)
	if len(pairs) == 0 {
		t.Fatal("no straddling (INSEE, CP) pair in the embedded table; the fixture this test is built on is gone")
	}
	for _, p := range pairs {
		if err := validateCoherence(GeocodeResult{CityCode: p.insee, PostCode: p.zip}); err != nil {
			t.Errorf("validateCoherence(INSEE %s, CP %s) = %v, want nil: the commune table says that postcode serves that commune", p.insee, p.zip, err)
		}
	}
	// The named example from the godoc, spelled out so a table refresh
	// that dropped it would be visible.
	if err := validateCoherence(GeocodeResult{CityCode: "04066", PostCode: "05110"}); err != nil {
		t.Errorf("validateCoherence(Curbans 04066, CP 05110) = %v, want nil", err)
	}
}

// TestValidateCoherence_RealDrift checks the guard still rejects what it
// exists for: a citycode and a postcode that do not belong together.
func TestValidateCoherence_RealDrift(t *testing.T) {
	cases := []struct {
		name       string
		insee, zip string
		wantErr    bool
	}{
		// Montreuil (93048) is served by 93100, not by a Loire code.
		{"cross_department_drift", "93048", "42100", true},
		// Same département, wrong commune: 93048 is not served by 93200
		// (Saint-Denis). The prefix comparison could never see this.
		{"same_department_wrong_commune", "93048", "93200", true},
		{"montreuil_ok", "93048", "93100", false},
		// Paris arrondissement INSEE, folded onto the parent commune.
		{"paris_arrondissement", "75111", "75011", false},
		{"paris_wrong_arrondissement", "75111", "93100", true},
		// Corsica: INSEE 2A/2B against postal 20, via the table.
		{"corsica_ajaccio", "2A004", "20000", false},
		// Unknown INSEE (younger than the snapshot): the prefix
		// fallback keeps the old carve-outs alive.
		{"unknown_insee_same_prefix", "99999", "99000", false},
		{"unknown_insee_domtom", "97999", "98800", false},
		{"unknown_insee_drift", "99999", "12000", true},
		// Nothing to compare.
		{"no_citycode", "", "75011", false},
		{"no_postcode", "75056", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCoherence(GeocodeResult{CityCode: tc.insee, PostCode: tc.zip})
			if got := errors.Is(err, ErrIncoherentBANResponse); got != tc.wantErr {
				t.Errorf("validateCoherence(%q, %q) err = %v, want incoherent = %v", tc.insee, tc.zip, err, tc.wantErr)
			}
		})
	}
}

type inseeZip struct{ insee, zip string }

// straddlingPairs returns every (INSEE, postal code) pair of the embedded
// commune table whose first two characters disagree outside the Corsica
// and DOM-TOM carve-outs: exactly the set the old prefix comparison got
// wrong.
func straddlingPairs(t *testing.T) []inseeZip {
	t.Helper()
	var out []inseeZip
	forEachINSEEZip(t, func(insee, zip string) {
		if len(insee) < 2 || len(zip) < 2 {
			return
		}
		cc, pc := insee[:2], zip[:2]
		switch {
		case cc == pc,
			(cc == "2A" || cc == "2B") && pc == "20",
			(cc == "97" || cc == "98") && (pc == "97" || pc == "98"):
			return
		}
		out = append(out, inseeZip{insee, zip})
	})
	return out
}

var (
	zipToINSEEOnce sync.Once
	zipToINSEE     map[string]string
)

// inseeServedBy returns one INSEE code the embedded table says that
// postal code serves, or "" when it serves none. Test fixtures use it to
// build a BAN answer whose citycode and postcode belong together.
func inseeServedBy(zip string) string {
	zipToINSEEOnce.Do(func() {
		zipToINSEE = make(map[string]string, 40000)
		eachINSEEZip(func(insee, z string) {
			if _, seen := zipToINSEE[z]; !seen {
				zipToINSEE[z] = insee
			}
		})
	})
	return zipToINSEE[zip]
}

func forEachINSEEZip(t *testing.T, fn func(insee, zip string)) {
	t.Helper()
	eachINSEEZip(fn)
}

func eachINSEEZip(fn func(insee, zip string)) {
	cr := csv.NewReader(bytes.NewReader(communes.InseeCPCSVBytes()))
	cr.FieldsPerRecord = -1
	if _, err := cr.Read(); err != nil { // header
		return
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) || err != nil {
			return
		}
		if len(rec) < 2 || rec[0] == "" || rec[1] == "" {
			continue
		}
		fn(rec[0], rec[1])
		if len(rec) >= 3 && rec[2] != "" {
			for _, alt := range strings.Split(rec[2], "|") {
				if alt != "" {
					fn(rec[0], alt)
				}
			}
		}
	}
}

package communes

import (
	"strings"
	"testing"
)

// TestCityZip_KnownCommunes exercises the happy path on a handful of
// well-known (city, dept) pairs: Vierzon 18100, Saint-Amand-Montrond
// 18200, Perceneige 89260, Vincennes 94300. These are all single-INSEE
// / single-CP communes.
func TestCityZip_KnownCommunes(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	cases := []struct {
		name, dept, wantCP string
	}{
		{"Vierzon", "18", "18100"},
		{"Saint-Amand-Montrond", "18", "18200"},
		{"Perceneige", "89", "89260"},
		{"Vincennes", "94", "94300"},
		{"Levallois-Perret", "92", "92300"},
	}
	for _, tc := range cases {
		got, ok := tbl.CityZip(tc.name, tc.dept)
		if !ok {
			t.Errorf("CityZip(%q, %q) = (%q, false), want (%q, true)", tc.name, tc.dept, got, tc.wantCP)
			continue
		}
		if got != tc.wantCP {
			t.Errorf("CityZip(%q, %q) = %q, want %q", tc.name, tc.dept, got, tc.wantCP)
		}
	}
}

// TestCityZip_AccentAndCase exercises the normalization rules: accent
// folding, case folding, and punctuation tolerance must all converge
// on the same lookup result. L'Abergement-Clémenciat (01001) is a
// single-CP commune carrying accents — exactly the shape the licitor
// fallback faces on rural payloads.
func TestCityZip_AccentAndCase(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"L'Abergement-Clémenciat",
		"l'abergement-clémenciat",
		"L'ABERGEMENT-CLEMENCIAT",
		"L Abergement Clemenciat",
	}
	want := "01400"
	for _, in := range cases {
		got, ok := tbl.CityZip(in, "01")
		if !ok || got != want {
			t.Errorf("CityZip(%q,01) = (%q,%v), want (%q,true)", in, got, ok, want)
		}
	}
}

// TestCityZip_AmbiguousMultiCPRefuses asserts that multi-CP communes
// (other than the consolidated Paris case) also refuse. Évry-
// Courcouronnes (INSEE 91228) carries 91000 + 91080, so the
// conservative posture must hold.
func TestCityZip_AmbiguousMultiCPRefuses(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.CityZip("Évry-Courcouronnes", "91"); ok || got != "" {
		t.Errorf("CityZip(Évry-Courcouronnes,91) = (%q,%v), want (\"\",false) since CP is ambiguous", got, ok)
	}
}

// TestCityZip_MultiCPRefuses asserts the function returns "" when the
// resolved commune carries multiple postal codes. Paris (INSEE 75056)
// has 21 CPs (75001..75020 + 75116); the conservative posture is to
// refuse rather than guess.
func TestCityZip_MultiCPRefuses(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.CityZip("Paris", "75"); ok || got != "" {
		t.Errorf("CityZip(Paris,75) = (%q,%v), want (\"\",false)", got, ok)
	}
}

// TestCityZip_WrongDeptMisses asserts a city in the wrong department
// produces a miss. Vierzon is in dept 18; querying it under dept 75
// must not return its 18100 CP.
func TestCityZip_WrongDeptMisses(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.CityZip("Vierzon", "75"); ok || got != "" {
		t.Errorf("CityZip(Vierzon,75) = (%q,%v), want (\"\",false)", got, ok)
	}
}

// TestCityZip_UnknownCityMisses asserts an unknown city produces a
// miss without panicking.
func TestCityZip_UnknownCityMisses(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.CityZip("Atlantis-sur-Mer", "75"); ok || got != "" {
		t.Errorf("CityZip(Atlantis,75) = (%q,%v), want (\"\",false)", got, ok)
	}
}

// TestCityZip_EmptyInputsAndNil exercises the early-return branches:
// empty city, empty dept, nil receiver. None must panic; all must
// return the empty-zero pair.
func TestCityZip_EmptyInputsAndNil(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tbl.CityZip("", "75"); ok || got != "" {
		t.Errorf("CityZip(\"\", 75) = (%q,%v), want (\"\",false)", got, ok)
	}
	if got, ok := tbl.CityZip("Vierzon", ""); ok || got != "" {
		t.Errorf("CityZip(Vierzon,\"\") = (%q,%v), want (\"\",false)", got, ok)
	}
	var nilTbl *Table
	if got, ok := nilTbl.CityZip("Vierzon", "18"); ok || got != "" {
		t.Errorf("nil receiver = (%q,%v), want (\"\",false)", got, ok)
	}
}

// TestZipForINSEE covers the direct INSEE→CP lookup that backs CityZip.
// Includes the unambiguous case, the multi-CP case (75056 Paris), and
// the unknown-INSEE case.
func TestZipForINSEE(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	// Vierzon: single CP.
	cp, ambig, ok := tbl.ZipForINSEE("18279")
	if !ok || ambig || cp != "18100" {
		t.Errorf("ZipForINSEE(18279) = (%q,%v,%v), want (18100,false,true)", cp, ambig, ok)
	}
	// Paris (consolidated INSEE): multi-CP.
	cp, ambig, ok = tbl.ZipForINSEE("75056")
	if !ok || !ambig {
		t.Errorf("ZipForINSEE(75056) ambiguous=true expected, got (%q,%v,%v)", cp, ambig, ok)
	}
	// Unknown.
	cp, ambig, ok = tbl.ZipForINSEE("99999")
	if ok || cp != "" {
		t.Errorf("ZipForINSEE(99999) = (%q,%v,%v), want (\"\",_,false)", cp, ambig, ok)
	}
	// Empty.
	cp, _, ok = tbl.ZipForINSEE("")
	if ok || cp != "" {
		t.Errorf("ZipForINSEE(\"\") = (%q,%v), want (\"\",false)", cp, ok)
	}
}

// TestAltZipsForINSEE asserts the alternate-CP enumeration for Paris.
// Defensive-copy semantics are mirrored from CityDepts.
func TestAltZipsForINSEE(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	alts := tbl.AltZipsForINSEE("75056")
	if len(alts) < 20 {
		t.Errorf("Paris (75056) should expose 20+ alt zips, got %d", len(alts))
	}
	// Verify defensive copy.
	if len(alts) > 0 {
		alts[0] = "ZZZZZ"
		again := tbl.AltZipsForINSEE("75056")
		if len(again) == 0 || again[0] == "ZZZZZ" {
			t.Error("AltZipsForINSEE must return a defensive copy")
		}
	}
	// Single-CP commune: nil alts.
	if alts := tbl.AltZipsForINSEE("18279"); alts != nil {
		t.Errorf("Vierzon (18279) has no alt zips, got %v", alts)
	}
	// Nil receiver / empty.
	var nilTbl *Table
	if alts := nilTbl.AltZipsForINSEE("75056"); alts != nil {
		t.Errorf("nil receiver = %v, want nil", alts)
	}
}

// TestInseeCPCSVBytes is a drift-detector against a botched embed
// directive. Mirrors TestFranceCSVBytes.
func TestInseeCPCSVBytes(t *testing.T) {
	b := InseeCPCSVBytes()
	if len(b) == 0 {
		t.Fatal("embedded insee_cp CSV is empty")
	}
	if !strings.HasPrefix(string(b[:32]), "insee,cp") {
		t.Errorf("first 32 bytes don't start with the expected header: %q", string(b[:32]))
	}
}

// TestParseInseeCPCSV_Errors covers the structural failure modes of
// the CP parser: empty input and bad header. The body parser is
// exercised indirectly by the Default-table tests.
func TestParseInseeCPCSV_Errors(t *testing.T) {
	if _, err := parseInseeCPCSV(strings.NewReader("")); err == nil {
		t.Error("empty input should error")
	}
	if _, err := parseInseeCPCSV(strings.NewReader("foo,bar\n00001,18100\n")); err == nil {
		t.Error("bad header should error")
	}
	if _, err := parseInseeCPCSV(strings.NewReader("insee,cp\n")); err == nil {
		t.Error("header-only (empty body) should error")
	}
	// Happy mini-parse with an alt-CP row.
	store, err := parseInseeCPCSV(strings.NewReader(
		"insee,cp,cps_alt\n" +
			"00001,18100,\n" +
			"00002,75001,75002|75003\n",
	))
	if err != nil {
		t.Fatalf("parse mini: %v", err)
	}
	if cp, ok := store.primary["00001"]; !ok || cp != "18100" {
		t.Errorf("00001 = (%q,%v), want (18100,true)", cp, ok)
	}
	if alts := store.alts["00002"]; len(alts) != 2 || alts[0] != "75002" || alts[1] != "75003" {
		t.Errorf("00002 alts = %v, want [75002 75003]", alts)
	}
}

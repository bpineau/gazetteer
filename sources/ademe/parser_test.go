package ademe

import (
	"errors"
	"testing"
)

func TestParseList_Paris11(t *testing.T) {
	body := mustReadFixture(t, "list_paris11.json")
	rows, err := ParseList(body)
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected non-empty results")
	}

	// Spot-check the first row — corresponds to the most recent
	// "82 Rue de la Roquette" Paris 11 DPE captured live.
	r0 := rows[0]
	if r0.NumeroDPE == "" {
		t.Errorf("rows[0].NumeroDPE is empty")
	}
	if r0.EtiquetteDPE == "" {
		t.Errorf("rows[0].EtiquetteDPE is empty")
	}
	if r0.CodePostalBAN != "75011" {
		t.Errorf("rows[0].CodePostalBAN = %q, want 75011", r0.CodePostalBAN)
	}
	if r0.NomCommuneBAN != "Paris" {
		t.Errorf("rows[0].NomCommuneBAN = %q, want Paris", r0.NomCommuneBAN)
	}
	if r0.SurfaceHabitableLogement == nil || *r0.SurfaceHabitableLogement <= 0 {
		t.Errorf("rows[0].SurfaceHabitableLogement = %v", r0.SurfaceHabitableLogement)
	}
	if r0.AdresseBAN == "" {
		t.Errorf("rows[0].AdresseBAN is empty")
	}
	if r0.TypeBatiment != "appartement" {
		t.Errorf("rows[0].TypeBatiment = %q, want appartement", r0.TypeBatiment)
	}
	if r0.DateEtablissementDPE == "" {
		t.Errorf("rows[0].DateEtablissementDPE is empty")
	}
}

func TestParseList_Empty(t *testing.T) {
	rows, err := ParseList(mustReadFixture(t, "list_empty.json"))
	if err != nil {
		t.Fatalf("ParseList(empty): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want 0 rows, got %d", len(rows))
	}
}

func TestParseList_EmptyBody(t *testing.T) {
	_, err := ParseList(nil)
	if !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("ParseList(nil) = %v, want ErrEmptyBody", err)
	}
}

func TestParseList_Garbage(t *testing.T) {
	_, err := ParseList([]byte("not json"))
	if !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("ParseList(garbage) = %v, want ErrEmptyBody wrap", err)
	}
}

func TestPickBestByNumber_LeadingMatch(t *testing.T) {
	rows := []Row{
		{AdresseBAN: "78 Rue de la Roquette 75011 Paris", EtiquetteDPE: "E"},
		{AdresseBAN: "82 Rue de la Roquette 75011 Paris", EtiquetteDPE: "D"},
		{AdresseBAN: "84 Rue de la Roquette 75011 Paris", EtiquetteDPE: "C"},
	}
	idx, ok := PickBestByNumber(rows, "82", 0)
	if !ok || idx != 1 {
		t.Fatalf("PickBestByNumber(82) = (%d, %v), want (1, true)", idx, ok)
	}
	if i, ok := PickBestByNumber(rows, "78", 0); !ok || i != 0 {
		t.Errorf("PickBestByNumber(78) = (%d, %v), want (0, true)", i, ok)
	}
	if i, ok := PickBestByNumber(rows, "99", 0); ok || i != -1 {
		t.Errorf("PickBestByNumber(99) = (%d, %v), want (-1, false)", i, ok)
	}
	if i, ok := PickBestByNumber(rows, "", 0); ok || i != -1 {
		t.Errorf("PickBestByNumber(\"\") = (%d, %v), want (-1, false)", i, ok)
	}
}

func TestPickBestByNumber_DoesNotMatchOnPrefix(t *testing.T) {
	// "180 Rue" must not match "18" — digit boundary.
	rows := []Row{
		{AdresseBAN: "180 Rue X 75011 Paris"},
	}
	if i, ok := PickBestByNumber(rows, "18", 0); ok || i != -1 {
		t.Errorf("PickBestByNumber(18) on 180 = (%d, %v), want (-1, false)", i, ok)
	}
	if i, ok := PickBestByNumber(rows, "180", 0); !ok || i != 0 {
		t.Errorf("PickBestByNumber(180) on 180 = (%d, %v), want (0, true)", i, ok)
	}
}

func TestPickBestByNumber_LetterSuffix(t *testing.T) {
	rows := []Row{
		{AdresseBAN: "82B Rue X"},
	}
	if i, ok := PickBestByNumber(rows, "82", 0); !ok || i != 0 {
		t.Errorf("PickBestByNumber(82) on 82B = (%d, %v), want (0, true)", i, ok)
	}
}

func TestPickBestByNumber_RangeRightBound(t *testing.T) {
	rows := []Row{
		{AdresseBAN: "80-82 Rue X"},
		{AdresseBAN: "100/102 Rue Y"},
		{AdresseBAN: "200 - 204 Rue Z"},
		{AdresseBAN: "10,12 Rue W"},
	}
	cases := []struct {
		num string
		idx int
	}{
		{"82", 0},
		{"102", 1},
		{"204", 2},
		{"12", 3},
		{"99", -1},
	}
	for _, tc := range cases {
		i, ok := PickBestByNumber(rows, tc.num, 0)
		if (tc.idx == -1 && (ok || i != -1)) || (tc.idx >= 0 && (!ok || i != tc.idx)) {
			t.Errorf("PickBestByNumber(%q) = (%d, %v), want (%d, %v)", tc.num, i, ok, tc.idx, tc.idx >= 0)
		}
	}
}

func TestPickBestByNumber_FallbackToAdresseBrut(t *testing.T) {
	// AdresseBAN empty but AdresseBrut starts with the right number.
	rows := []Row{
		{AdresseBrut: "82 RUE DE LA ROQUETTE"},
	}
	if i, ok := PickBestByNumber(rows, "82", 0); !ok || i != 0 {
		t.Errorf("PickBestByNumber on AdresseBrut = (%d, %v), want (0, true)", i, ok)
	}
}

func TestPickBest_PrefersFilledEtiquette(t *testing.T) {
	rows := []Row{
		{NumeroDPE: "a"},
		{NumeroDPE: "b", EtiquetteDPE: "D"},
	}
	idx, ok := PickBest(rows, 0)
	if !ok || idx != 1 {
		t.Fatalf("PickBest = (%d, %v), want (1, true)", idx, ok)
	}
}

func TestPickBest_EmptyEtiquetteFallsBackToZero(t *testing.T) {
	rows := []Row{{NumeroDPE: "a"}}
	idx, ok := PickBest(rows, 0)
	if !ok || idx != 0 {
		t.Fatalf("PickBest = (%d, %v), want (0, true)", idx, ok)
	}
}

func TestPickBest_Empty(t *testing.T) {
	idx, ok := PickBest(nil, 0)
	if ok || idx != -1 {
		t.Fatalf("PickBest(nil) = (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestPickBestByNumber_SurfaceTieBreak(t *testing.T) {
	// Apartment building: three DPE rows at "82 RUE X", different
	// surfaces. The caller's surface is 46 → the row with
	// SurfaceHabitableLogement closest to 46 must win.
	s38 := 38.0
	s48 := 48.0
	s103 := 103.0
	rows := []Row{
		{AdresseBAN: "82 Rue X", EtiquetteDPE: "F", SurfaceHabitableLogement: &s103},
		{AdresseBAN: "82 Rue X", EtiquetteDPE: "E", SurfaceHabitableLogement: &s38},
		{AdresseBAN: "82 Rue X", EtiquetteDPE: "D", SurfaceHabitableLogement: &s48},
	}
	idx, ok := PickBestByNumber(rows, "82", 46)
	if !ok || idx != 2 {
		t.Errorf("PickBestByNumber(82, 46) = (%d, %v), want (2, true)", idx, ok)
	}
	// Caller wants 100 m² → picks the 103 m² row.
	if i, _ := PickBestByNumber(rows, "82", 100); i != 0 {
		t.Errorf("PickBestByNumber(82, 100) = %d, want 0", i)
	}
	// wantSurface == 0 keeps the historical "first match" behaviour.
	if i, _ := PickBestByNumber(rows, "82", 0); i != 0 {
		t.Errorf("PickBestByNumber(82, 0) = %d, want 0 (first match)", i)
	}
}

func TestPickBestByNumber_SurfaceTieBreak_IgnoresRowsWithoutSurface(t *testing.T) {
	// Two rows match the number; one has no surface. The other one
	// wins regardless of how close its surface is, because the
	// surface-less row can't be ranked.
	s500 := 500.0
	rows := []Row{
		{AdresseBAN: "82 Rue X", EtiquetteDPE: "G"},
		{AdresseBAN: "82 Rue X", EtiquetteDPE: "F", SurfaceHabitableLogement: &s500},
	}
	if i, _ := PickBestByNumber(rows, "82", 46); i != 1 {
		t.Errorf("PickBestByNumber(82, 46) = %d, want 1 (only ranked row)", i)
	}
}

func TestPickBest_SurfaceTieBreak(t *testing.T) {
	s38 := 38.0
	s48 := 48.0
	s103 := 103.0
	rows := []Row{
		{EtiquetteDPE: "F", SurfaceHabitableLogement: &s103},
		{EtiquetteDPE: "E", SurfaceHabitableLogement: &s38},
		{EtiquetteDPE: "D", SurfaceHabitableLogement: &s48},
	}
	if i, _ := PickBest(rows, 46); i != 2 {
		t.Errorf("PickBest(rows, 46) = %d, want 2 (closest to 46)", i)
	}
	if i, _ := PickBest(rows, 0); i != 0 {
		t.Errorf("PickBest(rows, 0) = %d, want 0 (no surface tie-break)", i)
	}
}

func TestPickConfidence(t *testing.T) {
	cases := []struct {
		name      string
		matched   bool
		num       bool
		etiquette string
		want      string
	}{
		{"unmatched", false, false, "", ConfidenceLow},
		{"num+etiquette", true, true, "D", ConfidenceHigh},
		{"num only", true, true, "", ConfidenceMedium},
		{"etiquette only", true, false, "F", ConfidenceMedium},
		{"matched but neither", true, false, "", ConfidenceLow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PickConfidence(tc.matched, tc.num, tc.etiquette); got != tc.want {
				t.Errorf("PickConfidence(%v,%v,%q) = %q, want %q",
					tc.matched, tc.num, tc.etiquette, got, tc.want)
			}
		})
	}
}

func TestMatchAddrNumber_Empty(t *testing.T) {
	if matchAddrNumber("", "82") {
		t.Error("matchAddrNumber(empty, 82) = true, want false")
	}
	if matchAddrNumber("82 Rue", "") {
		t.Error("matchAddrNumber(addr, empty) = true, want false")
	}
}

func TestBuildResult_NilSafeOnSparseRow(t *testing.T) {
	r := buildResult(Row{})
	if r.DPE != nil || r.Logement != nil || r.Adresse != nil {
		t.Errorf("expected all-nil sub-blobs on empty Row, got %+v", r)
	}
}

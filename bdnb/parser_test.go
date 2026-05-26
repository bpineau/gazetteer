package bdnb

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
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}

	// Spot-check the first row.
	r0 := rows[0]
	if r0.BatimentGroupeID != "bdnb-bg-WX9A-TTJK-F8JY" {
		t.Errorf("rows[0].BatimentGroupeID = %q", r0.BatimentGroupeID)
	}
	if r0.CleInteropAdrPrincipale != "75111_6507_00003" {
		t.Errorf("rows[0].CleInteropAdrPrincipale = %q", r0.CleInteropAdrPrincipale)
	}
	if r0.CodeCommuneINSEE != "75111" {
		t.Errorf("rows[0].CodeCommuneINSEE = %q", r0.CodeCommuneINSEE)
	}
	if r0.AnneeConstruction == nil || *r0.AnneeConstruction != 1890 {
		t.Errorf("rows[0].AnneeConstruction = %v, want 1890", r0.AnneeConstruction)
	}
	if r0.NbLog == nil || *r0.NbLog != 2 {
		t.Errorf("rows[0].NbLog = %v, want 2", r0.NbLog)
	}
	if r0.ClasseConsoEnergieArrete2012 != "C" {
		t.Errorf("rows[0].ClasseConsoEnergieArrete2012 = %q", r0.ClasseConsoEnergieArrete2012)
	}
	if r0.DistanceMonumentHistorique == nil || *r0.DistanceMonumentHistorique != 105 {
		t.Errorf("rows[0].DistanceMonumentHistorique = %v, want 105", r0.DistanceMonumentHistorique)
	}
	if r0.PerimetreBatHistorique == nil || !*r0.PerimetreBatHistorique {
		t.Errorf("rows[0].PerimetreBatHistorique = %v, want true", r0.PerimetreBatHistorique)
	}
	if r0.ContrainteUrbanismeAC1 == nil || !*r0.ContrainteUrbanismeAC1 {
		t.Errorf("rows[0].ContrainteUrbanismeAC1 = %v, want true", r0.ContrainteUrbanismeAC1)
	}
	if got := len(r0.LParcelleID); got != 1 || r0.LParcelleID[0] != "75111000BR0052" {
		t.Errorf("rows[0].LParcelleID = %v", r0.LParcelleID)
	}
	if r0.TypeEnergieChauffage != "gaz" {
		t.Errorf("rows[0].TypeEnergieChauffage = %q", r0.TypeEnergieChauffage)
	}
	if r0.FiabiliteCRAdrNiv1 == "" {
		t.Errorf("rows[0].FiabiliteCRAdrNiv1 is empty")
	}

	r1 := rows[1]
	if r1.BatimentGroupeID != "bdnb-bg-2HLW-RFWC-JZVV" {
		t.Errorf("rows[1].BatimentGroupeID = %q", r1.BatimentGroupeID)
	}
	if r1.NbLog == nil || *r1.NbLog != 11 {
		t.Errorf("rows[1].NbLog = %v, want 11", r1.NbLog)
	}
	if r1.ValeurFonciereM2RelCommune != nil {
		t.Errorf("rows[1].ValeurFonciereM2RelCommune = %v, want nil", r1.ValeurFonciereM2RelCommune)
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

func TestPickBest_BANIDExact(t *testing.T) {
	rows := []Row{
		{CleInteropAdrPrincipale: "AAA"},
		{CleInteropAdrPrincipale: "BBB"},
		{CleInteropAdrPrincipale: "CCC"},
	}
	idx, ok := PickBest(rows, "BBB")
	if !ok || idx != 1 {
		t.Fatalf("PickBest = (%d, %v), want (1, true)", idx, ok)
	}
}

func TestPickBest_BANIDCaseInsensitive(t *testing.T) {
	rows := []Row{
		{CleInteropAdrPrincipale: "75111_6507_00003"},
	}
	idx, ok := PickBest(rows, "75111_6507_00003")
	if !ok || idx != 0 {
		t.Fatalf("PickBest = (%d, %v), want (0, true)", idx, ok)
	}
}

func TestPickBest_FallbackCompleteness(t *testing.T) {
	year := 1990
	nb := 5
	dist := 100
	rows := []Row{
		{BatimentGroupeID: "a"},
		{
			BatimentGroupeID:           "b",
			AnneeConstruction:          &year,
			NbLog:                      &nb,
			DistanceMonumentHistorique: &dist,
		},
	}
	idx, ok := PickBest(rows, "")
	if !ok || idx != 1 {
		t.Fatalf("PickBest(no want) = (%d, %v), want (1, true)", idx, ok)
	}
}

func TestPickBest_Empty(t *testing.T) {
	idx, ok := PickBest(nil, "")
	if ok || idx != -1 {
		t.Fatalf("PickBest(nil) = (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFlexString_DecodeVariants(t *testing.T) {
	body := []byte(`[
        {"quartier_prioritaire":null},
        {"quartier_prioritaire":true},
        {"quartier_prioritaire":false},
        {"quartier_prioritaire":"Grand Centre - Sémard"},
        {"quartier_prioritaire":42},
        {"quartier_prioritaire":["A"]},
        {"quartier_prioritaire":[]},
        {"quartier_prioritaire":[42]}
    ]`)
	rows, err := ParseList(body)
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if len(rows) != 8 {
		t.Fatalf("len = %d", len(rows))
	}
	want := []string{"", "true", "false", "Grand Centre - Sémard", "42", "A", "", "42"}
	for i, w := range want {
		if got := string(rows[i].QuartierPrioritaire); got != w {
			t.Errorf("rows[%d].QuartierPrioritaire = %q, want %q", i, got, w)
		}
	}
}

func TestPickBestByNumber(t *testing.T) {
	rows := []Row{
		{LibelleAdrPrincipale: "8 Rue Aubert 93200 Saint-Denis"},
		{LibelleAdrPrincipale: "9 Rue Aubert 93200 Saint-Denis"},
		{LibelleAdrPrincipale: "10 Rue Aubert 93200 Saint-Denis"},
		{LibelleAdrPrincipale: "120 Avenue Voltaire 75011 Paris"},
	}
	tests := []struct {
		want string
		idx  int
		ok   bool
	}{
		{"9", 1, true},
		{"10", 2, true},
		{"120", 3, true},
		{"12", -1, false},
		{"99", -1, false},
		{"", -1, false},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			i, ok := PickBestByNumber(rows, tc.want)
			if i != tc.idx || ok != tc.ok {
				t.Errorf("PickBestByNumber(%q) = (%d, %v), want (%d, %v)", tc.want, i, ok, tc.idx, tc.ok)
			}
		})
	}
}

func TestPickConfidence(t *testing.T) {
	tests := []struct {
		name      string
		matched   bool
		exact     bool
		fiabilite string
		want      string
	}{
		{"unmatched", false, false, "", "low"},
		{"exact+reliable", true, true, "données croisées à l'adresse fiables", "high"},
		{"exact+degraded", true, true, "non fiable", "medium"},
		{"ilike+reliable", true, false, "données croisées à l'adresse fiables", "medium"},
		{"ilike+degraded", true, false, "", "low"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PickConfidence(tc.matched, tc.exact, tc.fiabilite); got != tc.want {
				t.Errorf("PickConfidence(%v,%v,%q) = %q, want %q",
					tc.matched, tc.exact, tc.fiabilite, got, tc.want)
			}
		})
	}
}

func TestBuildResult_NilSafeOnSparseRow(t *testing.T) {
	out := BuildResult(Row{})
	if out.Identity != nil || out.Building != nil || out.DPE != nil || out.Risks != nil || out.Fiabilite != nil {
		t.Errorf("expected all-nil sub-blobs on empty Row, got %+v", out)
	}
}

func TestBuildResult_DistributionPrefer2021(t *testing.T) {
	a, b := 1, 2
	r := Row{
		NbClasseBilanDPEA:               &a,
		NbClasseBilanDPEB:               &b,
		NbClasseConsoEnergieArrete2012A: &b, // would lose to 2021 if both populated
	}
	out := BuildResult(r)
	if out.DPE == nil {
		t.Fatal("DPE is nil")
	}
	if got := out.DPE.DistributionClasses["A"]; got != 1 {
		t.Errorf("DistributionClasses[A] = %d, want 1 (2021 wins)", got)
	}
}

func TestBuildResult_Fallback2012WhenNoBilan(t *testing.T) {
	c := 5
	r := Row{NbClasseConsoEnergieArrete2012C: &c}
	out := BuildResult(r)
	if out.DPE == nil {
		t.Fatal("DPE is nil")
	}
	if got := out.DPE.DistributionClasses["C"]; got != 5 {
		t.Errorf("DistributionClasses[C] = %d, want 5 (2012 fallback)", got)
	}
}

package dvf

import (
	"testing"
	"time"
)

// mkMut builds a synthetic DVF Mutation row with the minimal fields
// FilterMutations reads.
func mkMut(nature, typeLocal, date string, surface, valeur float64) Mutation {
	s := surface
	v := valeur
	return Mutation{
		IDMutation:        nature + "-" + date,
		DateMutation:      date,
		NatureMutation:    nature,
		TypeLocal:         typeLocal,
		SurfaceReelleBati: &s,
		ValeurFonciere:    &v,
	}
}

func TestMapPropertyTypeToDVF(t *testing.T) {
	cases := map[string]string{
		"apartment":  "Appartement",
		"house":      "Maison",
		"commercial": "Local industriel. commercial ou assimilé",
		"parking":    "",
		"land":       "",
		"unknown":    "",
		"":           "",
	}
	for in, want := range cases {
		if got := MapPropertyTypeToDVF(in); got != want {
			t.Errorf("MapPropertyTypeToDVF(%q) = %q want %q", in, got, want)
		}
	}
}

// TestFilterMutations_NatureMutationFilter pins the post-fix contract:
// FilterMutations drops every row whose nature_mutation is not "Vente".
func TestFilterMutations_NatureMutationFilter(t *testing.T) {
	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []Mutation{
		mkMut("Vente", "Appartement", "2024-06-01", 50, 300_000),
		mkMut("Vente", "Appartement", "2024-07-01", 60, 360_000),
		mkMut("Vente en l'état futur d'achèvement", "Appartement", "2024-06-01", 55, 440_000),
		mkMut("Adjudication", "Appartement", "2024-06-01", 50, 180_000),
		mkMut("Echange", "Appartement", "2024-06-01", 50, 250_000),
		mkMut("Vente terrain à bâtir", "Appartement", "2024-06-01", 50, 250_000),
		mkMut("Expropriation", "Appartement", "2024-06-01", 50, 250_000),
		mkMut("", "Appartement", "2024-06-01", 50, 250_000),
	}
	got := FilterMutations(in, "Appartement", cutoff)
	if len(got) != 2 {
		t.Fatalf("FilterMutations returned %d rows, want 2 (only Vente survives)", len(got))
	}
	for _, m := range got {
		if m.NatureMutation != NatureMutationVente {
			t.Errorf("non-Vente leaked through filter: %q", m.NatureMutation)
		}
	}
}

func TestFilterMutations_SurfaceBounds(t *testing.T) {
	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []Mutation{
		mkMut("Vente", "Appartement", "2024-06-01", 5, 100_000),    // too small
		mkMut("Vente", "Appartement", "2024-06-01", 50, 300_000),   // ok
		mkMut("Vente", "Appartement", "2024-06-01", 1500, 800_000), // too big
	}
	got := FilterMutations(in, "Appartement", cutoff)
	if len(got) != 1 {
		t.Fatalf("FilterMutations = %d rows, want 1", len(got))
	}
}

func TestFilterMutations_TypeLocalCaseInsensitive(t *testing.T) {
	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []Mutation{
		mkMut("Vente", "appartement", "2024-06-01", 50, 300_000), // lowercase
		mkMut("Vente", "APPARTEMENT", "2024-06-01", 60, 360_000), // uppercase
		mkMut("Vente", "Maison", "2024-06-01", 80, 400_000),      // different type
	}
	got := FilterMutations(in, "Appartement", cutoff)
	if len(got) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %d", len(got))
	}
}

func TestCapPerParcelle(t *testing.T) {
	mk := func(id string) Mutation { return Mutation{IDParcelle: id} }
	in := []Mutation{
		mk("A"), mk("A"), mk("A"), mk("A"), mk("A"), mk("A"), // 6 same id
		mk("B"), mk("B"),
		mk(""), mk(""), // empty id_parcelle: each unique
	}
	got := capPerParcelle(in, 4)
	// Expected: 4×A + 2×B + 2×empty = 8.
	if len(got) != 8 {
		t.Errorf("capPerParcelle(max=4) = %d rows, want 8", len(got))
	}
}

func TestCountUniqueParcelles(t *testing.T) {
	in := []Mutation{
		{IDParcelle: "A"},
		{IDParcelle: "A"},
		{IDParcelle: "B"},
		{IDParcelle: ""},
		{IDParcelle: ""}, // each empty counts unique
	}
	got := CountUniqueParcelles(in)
	if got != 4 {
		t.Errorf("CountUniqueParcelles = %d, want 4 (A + B + 2 empty)", got)
	}
}

func TestPerM2Quartiles(t *testing.T) {
	mk := func(v, s float64) Mutation {
		vv := v
		ss := s
		return Mutation{ValeurFonciere: &vv, SurfaceReelleBati: &ss}
	}
	// Per-m² values: 2000, 3000, 4000, 5000, 6000.
	in := []Mutation{
		mk(100_000, 50), // 2000
		mk(150_000, 50), // 3000
		mk(200_000, 50), // 4000
		mk(250_000, 50), // 5000
		mk(300_000, 50), // 6000
	}
	p25, p50, p75 := PerM2Quartiles(in)
	if p50 != 4000 {
		t.Errorf("median = %v, want 4000", p50)
	}
	if p25 >= p50 || p75 <= p50 {
		t.Errorf("quartiles ordering broken: p25=%v p50=%v p75=%v", p25, p50, p75)
	}
}

func TestPickConfidence(t *testing.T) {
	cases := []struct {
		n     int
		level string
		want  string
	}{
		{50, "commune", ConfidenceHigh},
		{50, "address_radius", ConfidenceHigh},
		{50, "neighborhood", ConfidenceMedium}, // capped at medium for multi-INSEE
		{15, "commune", ConfidenceMedium},
		{5, "commune", ConfidenceLow},
		{0, "department", ConfidenceLow},
	}
	for _, tc := range cases {
		if got := PickConfidence(tc.n, tc.level); got != tc.want {
			t.Errorf("PickConfidence(%d,%s)=%s want %s", tc.n, tc.level, got, tc.want)
		}
	}
}

func TestPerM2Quartiles_Empty(t *testing.T) {
	p25, p50, p75 := PerM2Quartiles(nil)
	if p25 != 0 || p50 != 0 || p75 != 0 {
		t.Errorf("empty input quartiles = (%v, %v, %v), want all 0", p25, p50, p75)
	}
}

func TestMutation_Valeur_Surface_Nil(t *testing.T) {
	m := Mutation{}
	if m.Valeur() != 0 {
		t.Errorf("Valeur() on zero mutation = %v, want 0", m.Valeur())
	}
	if m.Surface() != 0 {
		t.Errorf("Surface() on zero mutation = %v, want 0", m.Surface())
	}
}

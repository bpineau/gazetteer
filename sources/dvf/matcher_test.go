package dvf

import (
	"fmt"
	"testing"
	"time"
)

// mkMut builds a synthetic DVF Mutation row with the minimal fields
// FilterMutations reads. Every row gets its OWN id_mutation, derived from
// all of its fields: these rows stand for independent sales, and sharing an
// id would make them one multi-lot mutation, which FilterMutations rightly
// drops. Use mkLot to build the rows of a single mutation on purpose.
func mkMut(nature, typeLocal, date string, surface, valeur float64) Mutation {
	m := mkLot("", nature, typeLocal, date, surface, valeur)
	m.IDMutation = fmt.Sprintf("%s|%s|%s|%g|%g", nature, typeLocal, date, surface, valeur)
	return m
}

// mkLot builds one row of the mutation named by id — the shape geo-dvf uses
// for a sale bundling several locals, where valeur_fonciere is the price of
// the WHOLE mutation repeated on every row.
func mkLot(id, nature, typeLocal, date string, surface, valeur float64) Mutation {
	s := surface
	v := valeur
	return Mutation{
		IDMutation:        id,
		DateMutation:      date,
		NatureMutation:    nature,
		TypeLocal:         typeLocal,
		SurfaceReelleBati: &s,
		ValeurFonciere:    &v,
	}
}

// TestFilterMutations_SingleBuiltLocal pins the mutation-grouping rule:
// geo-dvf repeats the whole mutation's valeur_fonciere on each of its rows,
// so a multi-lot sale must contribute nothing rather than one bogus €/m² per
// lot. Shape taken from the packaged Paris 7e fixture (2024-1216400: two
// flats, 64 and 134 m², 3 010 000 € for the pair).
func TestFilterMutations_SingleBuiltLocal(t *testing.T) {
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
	in := []Mutation{
		mkLot("bundle", "Vente", "Appartement", "2024-06-01", 64, 3_010_000),
		mkLot("bundle", "Vente", "Appartement", "2024-06-01", 134, 3_010_000),
		mkLot("mixed", "Vente", "Appartement", "2024-06-01", 70, 900_000),
		mkLot("mixed", "Vente", "Local industriel. commercial ou assimilé", "2024-06-01", 200, 900_000),
		mkLot("solo", "Vente", "Appartement", "2024-06-01", 50, 500_000),
		// A bundled Dépendance carries no surface and must NOT disqualify.
		mkLot("withcave", "Vente", "Appartement", "2024-06-01", 60, 620_000),
		mkLot("withcave", "Vente", "Dépendance", "2024-06-01", 0, 620_000),
	}
	got := FilterMutations(in, "Appartement", window)
	if len(got) != 2 {
		t.Fatalf("FilterMutations = %d rows, want 2 (solo + withcave)", len(got))
	}
	for _, m := range got {
		if m.IDMutation == "bundle" || m.IDMutation == "mixed" {
			t.Errorf("multi-built-local mutation %q leaked through", m.IDMutation)
		}
	}
	// The dropped bundle would have published 47 031 and 22 463 €/m² for a
	// sale that transacted at 15 202.
	if _, p50, _ := PerM2Quartiles(got); p50 < 10_000 || p50 > 10_400 {
		t.Errorf("median = %.0f €/m², want the two clean lots' 10 000..10 400", p50)
	}
}

// TestIsBuiltLocal pins the vocabulary both DVF-backed Sources share.
func TestIsBuiltLocal(t *testing.T) {
	for in, want := range map[string]bool{
		"Appartement": true,
		"appartement": true,
		"Maison":      true,
		"Local industriel. commercial ou assimilé": true,
		"Dépendance": false,
		"":           false,
		"Terrain":    false,
	} {
		if got := IsBuiltLocal(in); got != want {
			t.Errorf("IsBuiltLocal(%q) = %v, want %v", in, got, want)
		}
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
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
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
	got := FilterMutations(in, "Appartement", window)
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
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
	in := []Mutation{
		mkMut("Vente", "Appartement", "2024-06-01", 5, 100_000),    // too small
		mkMut("Vente", "Appartement", "2024-06-01", 50, 300_000),   // ok
		mkMut("Vente", "Appartement", "2024-06-01", 1500, 800_000), // too big
	}
	got := FilterMutations(in, "Appartement", window)
	if len(got) != 1 {
		t.Fatalf("FilterMutations = %d rows, want 1", len(got))
	}
}

func TestFilterMutations_TypeLocalCaseInsensitive(t *testing.T) {
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
	in := []Mutation{
		mkMut("Vente", "appartement", "2024-06-01", 50, 300_000), // lowercase
		mkMut("Vente", "APPARTEMENT", "2024-06-01", 60, 360_000), // uppercase
		mkMut("Vente", "Maison", "2024-06-01", 80, 400_000),      // different type
	}
	got := FilterMutations(in, "Appartement", window)
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

// TestFilterMutations_WindowUpperBound pins the as-of contract: a cohort
// drawn as of a past date must not contain sales made after it. The floor
// alone used to let a 2022 query answer with 2025 prices.
func TestFilterMutations_WindowUpperBound(t *testing.T) {
	asOf := time.Date(2022, 6, 30, 0, 0, 0, 0, time.UTC)
	in := []Mutation{
		mkMut("Vente", "Appartement", "2016-06-01", 50, 300_000), // before the floor
		mkMut("Vente", "Appartement", "2019-06-01", 50, 300_000), // inside
		mkMut("Vente", "Appartement", "2022-06-30", 50, 300_000), // the bound itself, inclusive
		mkMut("Vente", "Appartement", "2022-07-01", 50, 600_000), // after the reference date
		mkMut("Vente", "Appartement", "2025-12-12", 50, 900_000), // well after
	}
	got := FilterMutations(in, "Appartement", WindowEndingAt(asOf))
	if len(got) != 2 {
		t.Fatalf("FilterMutations = %d rows, want 2 (2019 and the bound itself)", len(got))
	}
	for _, m := range got {
		if m.DateMutation > "2022-06-30" {
			t.Errorf("mutation dated %s is after the as-of date", m.DateMutation)
		}
	}
	// A zero To keeps the old open-ended behaviour for callers that want it.
	open := FilterMutations(in, "Appartement", Window{From: asOf.AddDate(-CutoffYears, 0, 0)})
	if len(open) != 4 {
		t.Errorf("open-ended window = %d rows, want 4", len(open))
	}
}

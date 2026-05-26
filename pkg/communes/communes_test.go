package communes

import (
	"strings"
	"testing"
)

func TestDefault_Loads(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if tbl == nil {
		t.Fatal("nil table")
	}
	if got, _ := tbl.Lookup("75056"); got.Name == "" {
		// Paris (consolidated) entry should exist
		t.Errorf("expected lookup of 75056 to return a Commune")
	}
	if c, ok := tbl.Lookup("75107"); !ok {
		t.Fatalf("expected 75107 (Paris 7e) in table")
	} else if !strings.Contains(c.Name, "7e") {
		t.Errorf("75107 name = %q, want containing '7e'", c.Name)
	}
}

func TestNeighbors_Paris7e(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	got := tbl.Neighbors("75107", 5.0)
	if len(got) < 2 {
		t.Fatalf("expected at least 1 neighbour for Paris 7e (besides itself), got %v", got)
	}
	// Must include 75107 itself.
	containsSelf := false
	for _, id := range got {
		if id == "75107" {
			containsSelf = true
		}
	}
	if !containsSelf {
		t.Errorf("Neighbors must include self; got %v", got)
	}
	// Should include 75106 (Paris 6e) or 75108 (Paris 8e) — both within
	// 5 km of 75107.
	hasAdj := false
	for _, id := range got {
		if id == "75106" || id == "75108" {
			hasAdj = true
		}
	}
	if !hasAdj {
		t.Errorf("Neighbors(75107, 5) should include 75106 or 75108; got %v", got)
	}
}

func TestSameDepartment_Paris(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	got := tbl.SameDepartment("75107")
	if len(got) < 20 {
		t.Fatalf("Paris department should have ≥20 entries, got %d (%v)", len(got), got)
	}
	// All 20 arrondissements 75101..75120 must be present.
	want := []string{"75101", "75102", "75103", "75104", "75105", "75106", "75107", "75108", "75109", "75110",
		"75111", "75112", "75113", "75114", "75115", "75116", "75117", "75118", "75119", "75120"}
	gotSet := map[string]struct{}{}
	for _, id := range got {
		gotSet[id] = struct{}{}
	}
	for _, w := range want {
		if _, ok := gotSet[w]; !ok {
			t.Errorf("SameDepartment(75107) missing %s", w)
		}
	}
}

// TestNearestDept verifies the reverse-geocode helper used by the
// vench/licitor mappers to dept-guard their lat/lon emissions.
func TestNearestDept(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	cases := []struct {
		name     string
		lat, lon float64
		want     string // department code prefix expected
	}{
		{"paris_notre_dame", 48.8530, 2.3499, "75"},
		{"lyon_centre", 45.7640, 4.8357, "69"},
		{"marseille", 43.2965, 5.3698, "13"},
		{"saint_denis_93", 48.9362, 2.3574, "93"},
		{"reunion_saint_denis", -20.8823, 55.4504, "974"},
		{"zero_sentinel_empty", 0, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tbl.NearestDept(c.lat, c.lon)
			if got != c.want {
				t.Errorf("NearestDept(%v, %v) = %q, want %q", c.lat, c.lon, got, c.want)
			}
		})
	}
}

// TestNearestDept_NilTable verifies the nil-safety contract.
func TestNearestDept_NilTable(t *testing.T) {
	var tbl *Table
	if got := tbl.NearestDept(48.85, 2.35); got != "" {
		t.Errorf("nil table NearestDept = %q, want \"\"", got)
	}
}

func TestHaversineKm(t *testing.T) {
	// Paris (Notre-Dame) ≈ Versailles ≈ 17 km
	d := HaversineKm(48.8530, 2.3499, 48.8049, 2.1204)
	if d < 14 || d > 20 {
		t.Errorf("Notre-Dame → Versailles distance got %.2f, expected 14-20 km", d)
	}
	// Same point.
	if d := HaversineKm(48.85, 2.35, 48.85, 2.35); d != 0 {
		t.Errorf("same point should give 0, got %v", d)
	}
}

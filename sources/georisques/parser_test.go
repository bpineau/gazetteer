package georisques

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseReport_Paris11(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "paris11.json")
	r, err := ParseReport(body)
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if r.Adresse.Libelle != "13 Rue Alphonse Baudin, 75011 Paris" {
		t.Errorf("Adresse.Libelle = %q", r.Adresse.Libelle)
	}
	if got := r.Adresse.Latitude; got < 48.86 || got > 48.87 {
		t.Errorf("Adresse.Latitude = %v", got)
	}
	if got := r.Adresse.Longitude; got < 2.36 || got > 2.38 {
		t.Errorf("Adresse.Longitude = %v", got)
	}
	if r.Commune.CodeInsee != "75056" {
		t.Errorf("Commune.CodeInsee = %q", r.Commune.CodeInsee)
	}
	if r.Commune.CodePostal != "75011" {
		t.Errorf("Commune.CodePostal = %q", r.Commune.CodePostal)
	}
	if r.URL == "" {
		t.Error("URL is empty")
	}

	// Spot-check every risk we care about for the smoke run.
	if !r.RisquesNaturels.Inondation.Present {
		t.Error("Inondation should be present in Paris 11")
	}
	if r.RisquesNaturels.Inondation.LibelleStatutCommune != "Risque Existant" {
		t.Errorf("Inondation.statutCommune = %q", r.RisquesNaturels.Inondation.LibelleStatutCommune)
	}
	if !r.RisquesNaturels.Seisme.Present {
		t.Error("Seisme should be present")
	}
	if r.RisquesNaturels.Seisme.LibelleStatutCommune != "Risque Existant - faible" {
		t.Errorf("Seisme.statutCommune = %q", r.RisquesNaturels.Seisme.LibelleStatutCommune)
	}
	if !r.RisquesNaturels.RetraitGonflementArgile.Present {
		t.Error("RetraitGonflementArgile should be present")
	}
	if !r.RisquesTechnologiques.PollutionSols.Present {
		t.Error("PollutionSols should be present")
	}
	if r.RisquesTechnologiques.PollutionSols.LibelleStatutAdresse != "Risque Concerne" {
		t.Errorf("PollutionSols.statutAdresse = %q", r.RisquesTechnologiques.PollutionSols.LibelleStatutAdresse)
	}
	if r.RisquesTechnologiques.Nucleaire.Present {
		t.Error("Nucleaire should NOT be present in Paris 11")
	}
	if r.RisquesNaturels.Avalanche.Present {
		t.Error("Avalanche should NOT be present in Paris 11")
	}
}

func TestParseReport_Empty(t *testing.T) {
	t.Parallel()

	r, err := ParseReport(mustReadFixture(t, "empty.json"))
	if err != nil {
		t.Fatalf("ParseReport(empty): %v", err)
	}
	if r == nil {
		t.Fatal("nil report on {}")
	}
	if r.Adresse.Libelle != "" {
		t.Errorf("expected empty Adresse, got %q", r.Adresse.Libelle)
	}
	if r.Commune.CodeInsee != "" {
		t.Errorf("expected empty Commune, got %q", r.Commune.CodeInsee)
	}
}

func TestParseReport_NilBody(t *testing.T) {
	t.Parallel()

	if _, err := ParseReport(nil); !errors.Is(err, ErrEmptyBody) {
		t.Errorf("ParseReport(nil) = %v, want ErrEmptyBody", err)
	}
}

func TestParseReport_Garbage(t *testing.T) {
	t.Parallel()

	_, err := ParseReport([]byte("not json"))
	if !errors.Is(err, ErrEmptyBody) {
		t.Errorf("ParseReport(garbage) = %v, want ErrEmptyBody wrap", err)
	}
}

func TestCanonicalLists_StableOrder(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "paris11.json")
	r, err := ParseReport(body)
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	nat := CanonicalNaturels(r)
	if got, want := len(nat), 12; got != want {
		t.Fatalf("len(CanonicalNaturels) = %d, want %d", got, want)
	}
	if nat[0].Key != "inondation" || nat[3].Key != "seisme" || nat[11].Key != "radon" {
		t.Errorf("CanonicalNaturels order drifted: [0]=%q [3]=%q [11]=%q",
			nat[0].Key, nat[3].Key, nat[11].Key)
	}
	tech := CanonicalTechnos(r)
	if got, want := len(tech), 6; got != want {
		t.Fatalf("len(CanonicalTechnos) = %d, want %d", got, want)
	}
	if tech[0].Key != "icpe" || tech[3].Key != "pollution_sols" {
		t.Errorf("CanonicalTechnos order drifted: [0]=%q [3]=%q",
			tech[0].Key, tech[3].Key)
	}
}

func TestCanonicalLists_NilSafe(t *testing.T) {
	t.Parallel()

	if got := CanonicalNaturels(nil); got != nil {
		t.Errorf("CanonicalNaturels(nil) = %v, want nil", got)
	}
	if got := CanonicalTechnos(nil); got != nil {
		t.Errorf("CanonicalTechnos(nil) = %v, want nil", got)
	}
}

func TestStatutAdresseExisting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		risk Risk
		want bool
	}{
		{"empty", Risk{}, false},
		{"existant", Risk{LibelleStatutAdresse: "Risque Existant"}, true},
		{"existant_faible", Risk{LibelleStatutAdresse: "Risque Existant - faible"}, true},
		{"concerne", Risk{LibelleStatutAdresse: "Risque Concerne"}, true},
		{"non_concerne", Risk{LibelleStatutAdresse: "Risque non Concerne"}, false},
		{"non_connu", Risk{LibelleStatutAdresse: "Risque non Connu"}, false},
		{"lowercase_existant", Risk{LibelleStatutAdresse: "risque existant"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatutAdresseExisting(tc.risk); got != tc.want {
				t.Errorf("StatutAdresseExisting(%+v) = %v, want %v", tc.risk, got, tc.want)
			}
		})
	}
}

func TestStatutCommuneExisting(t *testing.T) {
	t.Parallel()

	if !StatutCommuneExisting(Risk{LibelleStatutCommune: "Risque Existant - important"}) {
		t.Error("expected Existant - important to be true at commune scale")
	}
	if StatutCommuneExisting(Risk{LibelleStatutCommune: "Risque non Concerne"}) {
		t.Error("expected 'Risque non Concerne' to be false")
	}
}

func TestContainsCI_HasPrefixCI_Smoke(t *testing.T) {
	t.Parallel()

	if !containsCI("Risque Existant", "existant") {
		t.Error("containsCI failed")
	}
	if containsCI("ABC", "z") {
		t.Error("containsCI false-positive")
	}
	if !hasPrefixCI("Risque non Concerne", "risque non") {
		t.Error("hasPrefixCI failed")
	}
	if hasPrefixCI("Risque", "risque non") {
		t.Error("hasPrefixCI false-positive (prefix longer than s)")
	}
}

func TestBuildResult_NilReport(t *testing.T) {
	t.Parallel()

	rb := BuildResult(nil)
	if rb.Confidence != ConfidenceLow {
		t.Errorf("Confidence(nil) = %q, want low", rb.Confidence)
	}
	if rb.Address != nil || rb.Commune != nil {
		t.Errorf("expected nil sub-blobs, got %+v", rb)
	}
	if rb.Summary.RedFlags == nil {
		t.Error("RedFlags should be non-nil empty slice on nil report")
	}
	if rb.LevelUsed != LevelCommune {
		t.Errorf("LevelUsed(nil) = %q, want %q", rb.LevelUsed, LevelCommune)
	}
}

// A86 H4 — level_used stamping. Symmetric with DVF / MA / Pappers Immo /
// Castorus. Address-scope when BRGM resolved the request to the building's
// exact lat/lon (Adresse.Libelle populated) ; commune-scope otherwise.
func TestBuildResult_LevelUsed(t *testing.T) {
	t.Parallel()

	t.Run("address_when_libelle_populated", func(t *testing.T) {
		r, err := ParseReport(mustReadFixture(t, "paris11.json"))
		if err != nil {
			t.Fatalf("ParseReport: %v", err)
		}
		if r.Adresse.Libelle == "" {
			t.Skip("fixture has no adresse libelle, can't exercise the address branch")
		}
		rb := BuildResult(r)
		if rb.LevelUsed != LevelAddress {
			t.Errorf("LevelUsed = %q, want %q", rb.LevelUsed, LevelAddress)
		}
	})

	t.Run("commune_when_libelle_empty", func(t *testing.T) {
		r := &Report{} // zero-valued — no Adresse.Libelle
		r.Commune.CodeInsee = "75111"
		rb := BuildResult(r)
		if rb.LevelUsed != LevelCommune {
			t.Errorf("LevelUsed = %q, want %q", rb.LevelUsed, LevelCommune)
		}
	})
}

func TestBuildResult_FixtureSummary(t *testing.T) {
	t.Parallel()

	r, err := ParseReport(mustReadFixture(t, "paris11.json"))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	rb := BuildResult(r)

	// Paris 11 has at least: inondation, remontee_nappe, seisme,
	// mouvement_terrain, retrait_argile, radon (= 6 naturels present).
	if rb.Summary.NaturelsPresentCount < 6 {
		t.Errorf("NaturelsPresentCount = %d, want >= 6", rb.Summary.NaturelsPresentCount)
	}
	// Technos present: ICPE, canalisations_md, pollution_sols.
	if rb.Summary.TechnosPresentCount < 3 {
		t.Errorf("TechnosPresentCount = %d, want >= 3", rb.Summary.TechnosPresentCount)
	}
	// At the address scale only the risks marked "Existant" /
	// "Concerne" qualify. From the fixture: inondation ("Risque
	// Existant"), remontee_nappe ("Risque Existant"), seisme ("Risque
	// Existant - faible"), radon ("Risque Existant - faible"),
	// pollution_sols ("Risque Concerne"). Note retrait_argile is
	// "Risque non Connu" at the address scale, so it should NOT be a
	// red flag despite being present at commune scale.
	mustHave := []string{"inondation", "remontee_nappe", "seisme", "radon", "pollution_sols"}
	for _, k := range mustHave {
		if !contains(rb.Summary.RedFlags, k) {
			t.Errorf("RedFlags missing %q: %v", k, rb.Summary.RedFlags)
		}
	}
	// Retrait argile is "Risque non Connu" → must NOT be in red flags.
	if contains(rb.Summary.RedFlags, "retrait_argile") {
		t.Errorf("RedFlags contains retrait_argile (Risque non Connu): %v", rb.Summary.RedFlags)
	}

	// Confidence: at least 1 risk present + Adresse populated → high.
	if rb.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", rb.Confidence)
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

package locservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustReadFixture reads a captured live HTML response (Latin-1 /
// ISO-8859-1 raw bytes — same as LocService serves).
func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParse_Paris7All_HasData(t *testing.T) {
	body := mustReadFixture(t, "paris7_all.html")
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.HasData {
		t.Fatalf("expected HasData=true, got false")
	}
	if got.TensionScore != 8 {
		t.Errorf("Paris 7 tension = %d, want 8", got.TensionScore)
	}
	if got.Label != LabelTresTendu {
		t.Errorf("Paris 7 label = %q, want %q", got.Label, LabelTresTendu)
	}
	if !got.HasBudget || got.BudgetScore != 5 {
		t.Errorf("Paris 7 budget = (has=%v, %d), want (true, 5)", got.HasBudget, got.BudgetScore)
	}
	if !strings.Contains(got.CityLabel, "Paris") {
		t.Errorf("CityLabel = %q, want containing 'Paris'", got.CityLabel)
	}
	if !strings.Contains(strings.ToLower(got.Description), "tendu") {
		t.Errorf("Description should mention 'tendu', got %q", got.Description)
	}
}

func TestParse_TroyesT2_HasData(t *testing.T) {
	body := mustReadFixture(t, "troyes_t2.html")
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.HasData {
		t.Fatalf("expected HasData=true, got false")
	}
	if got.TensionScore != 8 {
		t.Errorf("Troyes T2 tension = %d, want 8", got.TensionScore)
	}
	if got.BudgetScore != 5 {
		t.Errorf("Troyes T2 budget = %d, want 5", got.BudgetScore)
	}
}

func TestParse_LimogesAll_Equilibre(t *testing.T) {
	body := mustReadFixture(t, "limoges_all.html")
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.HasData {
		t.Fatal("expected HasData=true")
	}
	if got.TensionScore != 4 {
		t.Errorf("Limoges tension = %d, want 4", got.TensionScore)
	}
	if got.Label != LabelEquilibre {
		t.Errorf("Limoges label = %q, want %q", got.Label, LabelEquilibre)
	}
}

func TestParse_Paris7Chambre_Detendu(t *testing.T) {
	body := mustReadFixture(t, "paris7_chambre.html")
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.HasData {
		t.Fatal("expected HasData=true")
	}
	// fleche1 → "tres detendu"
	if got.TensionScore != 1 {
		t.Errorf("Paris 7 chambre tension = %d, want 1", got.TensionScore)
	}
	if got.Label != LabelTresDetendu {
		t.Errorf("Paris 7 chambre label = %q, want %q", got.Label, LabelTresDetendu)
	}
}

func TestParse_Riom_NoData(t *testing.T) {
	body := mustReadFixture(t, "riom_no_data.html")
	got, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.HasData {
		t.Fatal("expected HasData=false")
	}
	if got.NoDataMessage == "" {
		t.Errorf("expected NoDataMessage to be populated, got empty")
	}
}

func TestParse_Empty(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Error("expected error for empty body")
	}
	if _, err := Parse([]byte("<html><body>no markers</body></html>")); err == nil {
		t.Error("expected error for unrecognized body")
	}
}

func TestScoreToLabel(t *testing.T) {
	cases := []struct {
		score int
		want  TensionLabel
	}{
		{0, LabelTresDetendu},
		{1, LabelTresDetendu},
		{2, LabelDetendu},
		{3, LabelDetendu},
		{4, LabelEquilibre},
		{5, LabelTendu},
		{6, LabelTendu},
		{7, LabelTresTendu},
		{8, LabelTresTendu},
		{99, LabelEquilibre}, // out-of-range fallback
		{-1, LabelTresDetendu},
	}
	for _, tc := range cases {
		if got := ScoreToLabel(tc.score); got != tc.want {
			t.Errorf("ScoreToLabel(%d) = %q want %q", tc.score, got, tc.want)
		}
	}
}

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	if _, err := Parse(nil); err == nil {
		t.Error("expected error for empty body")
	}
	if _, err := Parse([]byte("<html><body>no markers</body></html>")); err == nil {
		t.Error("expected error for unrecognized body")
	}
}

func TestScoreToLabel(t *testing.T) {
	t.Parallel()

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

// The 2026-09 redesign: UTF-8 pages, CSS dials instead of arrow images.
// Every fixture below is a live capture of 2026-10-01. The expected
// scores were read off the needle angles, then cross-checked against
// the verdict zone the page prints next to each dial.
func TestParse_V2Dials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fixture         string
		tension, budget int
		label           TensionLabel
		city            string
		descWord        string
	}{
		// needles 0deg / 113deg, verdicts "Très difficile" / "Détendu"
		{"v2_paris02_all.html", 8, 5, LabelTresTendu, "Paris 02", "tendu"},
		// needles 0deg / 68deg, verdicts "Très difficile" / "Tendu"
		{"v2_paris02_t2.html", 8, 3, LabelTresTendu, "Paris 02", "tendu"},
		// needles 113deg / 113deg, verdicts "Facile" / "Détendu"
		{"v2_troyes_all.html", 3, 5, LabelDetendu, "Troyes", ""},
		// needles 180deg / 90deg, verdicts "Très facile" / "Équilibré"
		{"v2_limoges_chambre.html", 0, 4, LabelTresDetendu, "Limoges", ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got, err := Parse(mustReadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !got.HasData {
				t.Fatalf("expected HasData=true, got false (no-data message %q)", got.NoDataMessage)
			}
			if got.TensionScore != tc.tension || got.Label != tc.label {
				t.Errorf("tension = %d (%q), want %d (%q)", got.TensionScore, got.Label, tc.tension, tc.label)
			}
			if !got.HasBudget || got.BudgetScore != tc.budget {
				t.Errorf("budget = (has=%v, %d), want (true, %d)", got.HasBudget, got.BudgetScore, tc.budget)
			}
			if got.CityLabel != tc.city {
				t.Errorf("CityLabel = %q, want %q", got.CityLabel, tc.city)
			}
			if tc.descWord != "" && !strings.Contains(strings.ToLower(got.Description), tc.descWord) {
				t.Errorf("Description = %q, want containing %q", got.Description, tc.descWord)
			}
			if got.Description == "" {
				t.Errorf("Description is empty")
			}
		})
	}
}

func TestParse_V2NoData(t *testing.T) {
	t.Parallel()

	got, err := Parse(mustReadFixture(t, "v2_lyon_f3_no_data.html"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.HasData {
		t.Fatalf("expected HasData=false, got tension %d", got.TensionScore)
	}
	if !strings.Contains(got.NoDataMessage, "pas suffisamment actif") {
		t.Errorf("NoDataMessage = %q, want the no-data sentence", got.NoDataMessage)
	}
	if got.CityLabel != "Lyon" {
		t.Errorf("CityLabel = %q, want %q", got.CityLabel, "Lyon")
	}
}

// The dial arithmetic: nine positions 22.5 degrees apart, the tension
// dial reversed, the budget dial not, rounded angles accepted.
func TestDialScores(t *testing.T) {
	t.Parallel()

	page := func(a, b string) string {
		return `<span class="rental-tension-dial-needle" style="--rental-tension-angle: ` + a + `deg"></span>` +
			`<span class="rental-tension-dial-needle" style="--rental-tension-angle: ` + b + `deg"></span>`
	}
	cases := []struct {
		a, b string
		want []int
	}{
		{"0", "180", []int{8, 8}},
		{"180", "0", []int{0, 0}},
		{"90", "90", []int{4, 4}},
		{"113", "68", []int{3, 3}},
		{"157.5", "22.5", []int{1, 1}},
	}
	for _, tc := range cases {
		got, ok := dialScores(page(tc.a, tc.b))
		if !ok || len(got) != 2 || got[0] != tc.want[0] || got[1] != tc.want[1] {
			t.Errorf("dialScores(%s, %s) = %v, %v; want %v", tc.a, tc.b, got, ok, tc.want)
		}
	}
	if _, ok := dialScores(page("200", "0")); ok {
		t.Errorf("an angle past 180deg must be refused")
	}
	if _, ok := dialScores("<p>no dial</p>"); ok {
		t.Errorf("a page without a needle must report no dial")
	}
}

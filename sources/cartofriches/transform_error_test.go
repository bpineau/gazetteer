package cartofriches

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// listing is a one-field Listing: INSEE is the only input this Source reads.
func listing(insee string) gazetteer.Listing { return gazetteer.Listing{INSEE: insee} }

// csvHeader is the upstream friches-standard header, in the upstream's own
// column order (the transform resolves columns by name, not by position).
const csvHeader = `"site_id";"site_type";"site_statut";"comm_nom";"comm_insee";"unite_fonciere_surface"`

// writeRaw materialises a synthetic upstream CSV and returns a RawSet
// serving it, the way dataset.Refresh would.
func writeRaw(t *testing.T, body string) dataset.RawSet {
	t.Helper()
	path := filepath.Join(t.TempDir(), rawName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write raw: %v", err)
	}
	return fixtureRawSet{path}
}

// brokenRawSet fails to open, standing in for a missing/unreadable download.
type brokenRawSet struct{}

func (brokenRawSet) Open(string) (io.ReadCloser, error) { return nil, errors.New("boom: no raw file") }

func TestTransform_Failures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     dataset.RawSet
		body    string // used when raw is nil
		wantErr string
	}{
		{
			name:    "raw file unavailable",
			raw:     brokenRawSet{},
			wantErr: "boom: no raw file",
		},
		{
			name:    "empty file has no header",
			body:    "",
			wantErr: "read header",
		},
		{
			name:    "header missing the INSEE column",
			body:    `"site_id";"site_type";"site_statut";"comm_nom";"unite_fonciere_surface"` + "\n",
			wantErr: "header missing required columns",
		},
		{
			name:    "malformed quoting inside a row",
			body:    csvHeader + "\n" + `"a";"friche industrielle";"friche sans projet";"Ville"bad";"59350";10` + "\n",
			wantErr: "read row",
		},
		{
			name: "every row lacks a commune code",
			body: csvHeader + "\n" +
				`"a";"friche industrielle";"friche sans projet";"Ville";"";10` + "\n" +
				`"b";"friche industrielle";"friche sans projet";"Ville";NA;10` + "\n",
			wantErr: "produced no sites",
		},
		{
			name:    "header only",
			body:    csvHeader + "\n",
			wantErr: "produced no sites",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raw := c.raw
			if raw == nil {
				raw = writeRaw(t, c.body)
			}
			var buf bytes.Buffer
			err := transform(context.Background(), raw, &buf)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("transform = %v, want an error containing %q", err, c.wantErr)
			}
			if buf.Len() > 0 {
				t.Errorf("a failed transform wrote %d bytes, want none", buf.Len())
			}
		})
	}
}

// TestTransform_ShortRowAndUnits pins the row-level tolerances the aggregate
// relies on: a row shorter than the schema is read without panicking (the
// missing INSEE simply drops it), NA cells never become a category nor
// inflate the surface, and surfaces are summed as whole m².
func TestTransform_ShortRowAndUnits(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		`"short";"friche industrielle"` + "\n" + // 2 fields: no INSEE, dropped
		`"a";"friche industrielle";"friche avec projet";"Lille";"59350";"1200"` + "\n" +
		`"b";"friche industrielle";NA;"Lille";"59350";"800.9"` + "\n" +
		`"c";NA;"friche avec projet";"Lille";"59350";NA` + "\n"

	var buf bytes.Buffer
	if err := transform(context.Background(), writeRaw(t, body), &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if idx.Meta.RowCountSites != 3 || idx.Count() != 1 {
		t.Fatalf("meta = %d sites / %d communes, want 3 / 1", idx.Meta.RowCountSites, idx.Count())
	}
	e, ok := idx.Lookup("59350")
	if !ok {
		t.Fatal("59350 missing from the index")
	}
	if e.SiteCount != 3 {
		t.Errorf("SiteCount = %d, want 3", e.SiteCount)
	}
	if e.Label != "Lille" {
		t.Errorf("Label = %q, want Lille (first row seen)", e.Label)
	}
	// 1200 + 800 (decimal truncated) + NA (skipped), in m².
	if e.TotalSurfaceM2 != 2000 {
		t.Errorf("TotalSurfaceM2 = %d m², want 2000", e.TotalSurfaceM2)
	}
	if got := e.ByType["friche industrielle"]; got != 2 {
		t.Errorf("ByType[industrielle] = %d, want 2 (the NA type is not a category)", got)
	}
	if _, bad := e.ByType[naToken]; bad {
		t.Error("the NA sentinel leaked into ByType")
	}
	if got := e.ByStatus["friche avec projet"]; got != 2 {
		t.Errorf("ByStatus[avec projet] = %d, want 2", got)
	}
}

// TestTransform_UnescapesUpstreamQuotes: the upstream escapes quotes inside
// free-text cells the backslash way, which a strict CSV reader would
// otherwise mis-align onto the next column.
func TestTransform_UnescapesUpstreamQuotes(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		`"a";"friche d'habitat";"friche sans projet";"maison \"Les Opalines\"";"61386";"6507"` + "\n"
	var buf bytes.Buffer
	if err := transform(context.Background(), writeRaw(t, body), &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	e, ok := idx.Lookup("61386")
	if !ok {
		t.Fatal("61386 missing: the escaped quotes shifted the columns")
	}
	if e.Label != `maison "Les Opalines"` {
		t.Errorf("Label = %q, want the unescaped free text", e.Label)
	}
	if e.TotalSurfaceM2 != 6507 {
		t.Errorf("TotalSurfaceM2 = %d, want 6507", e.TotalSurfaceM2)
	}
}

func TestValidate_Failures(t *testing.T) {
	t.Parallel()
	gz := func(idx Index) []byte {
		t.Helper()
		var buf bytes.Buffer
		if err := dataset.WriteGzJSON(&buf, idx); err != nil {
			t.Fatalf("WriteGzJSON: %v", err)
		}
		return buf.Bytes()
	}
	cases := []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{"not gzip at all", []byte("plain text, not an artifact"), "cartofriches"},
		{"no communes", gz(Index{Meta: Meta{RowCountSites: 12}}), "no communes"},
		{
			name:    "communes but no sites",
			body:    gz(Index{Communes: map[string]Entry{"59350": {SiteCount: 1}}}),
			wantErr: "no sites",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := validate(bytes.NewReader(c.body))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("validate = %v, want an error containing %q", err, c.wantErr)
			}
		})
	}

	// The happy path, for symmetry: a plausible artifact validates.
	ok := gz(Index{
		Meta:     Meta{RowCountCommunes: 1, RowCountSites: 2},
		Communes: map[string]Entry{"59350": {SiteCount: 2}},
	})
	if err := validate(bytes.NewReader(ok)); err != nil {
		t.Errorf("validate(good artifact) = %v, want nil", err)
	}
}

func TestIndex_LookupGuards(t *testing.T) {
	t.Parallel()
	idx := &Index{Communes: map[string]Entry{"59350": {SiteCount: 4}}}
	cases := []struct {
		name  string
		idx   *Index
		insee string
		want  bool
	}{
		{"hit", idx, "59350", true},
		{"padded hit", idx, "  59350 ", true},
		{"miss", idx, "99999", false},
		{"empty code", idx, "", false},
		{"blank code", idx, "   ", false},
		{"nil index", nil, "59350", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := c.idx.Lookup(c.insee); ok != c.want {
				t.Errorf("Lookup(%q) ok = %v, want %v", c.insee, ok, c.want)
			}
		})
	}
	if got := idx.Count(); got != 1 {
		t.Errorf("Count = %d, want 1", got)
	}
	var nilIdx *Index
	if got := nilIdx.Count(); got != 0 {
		t.Errorf("nil Count = %d, want 0", got)
	}
}

func TestResult_IsEmptyNil(t *testing.T) {
	t.Parallel()
	var r *Result
	if !r.IsEmpty() {
		t.Error("(*Result)(nil).IsEmpty() = false, want true")
	}
	if (&Result{SiteCount: 1}).IsEmpty() {
		t.Error("a one-site Result must not report empty")
	}
}

func TestCopyIntMap_Nil(t *testing.T) {
	t.Parallel()
	if got := copyIntMap(nil); got != nil {
		t.Errorf("copyIntMap(nil) = %v, want nil (so omitempty still fires)", got)
	}
}

func TestSource_Accessors(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion || Version != sourceVersion {
		t.Errorf("Version = %d / %d, want %d", s.Version(), Version, sourceVersion)
	}
	sets := s.Datasets()
	if len(sets) != 1 || sets[0].Source != Name {
		t.Fatalf("Datasets = %+v, want the single %q set", sets, Name)
	}
	if sets[0].Processed.Name == "" || len(sets[0].Raw) != 1 || sets[0].Raw[0].URL == "" {
		t.Errorf("Datasets set is incomplete: %+v", sets[0])
	}
}

// TestQueryResult_EvidenceAndUnits pins the whole mapping on an injected
// index: counts, m² surface, the defensive breakdown copies, and every
// Evidence field (including the arrondissement folding the Cerema export
// forces on us).
func TestQueryResult_EvidenceAndUnits(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{RowCountSites: 41_234},
		Communes: map[string]Entry{
			"75056": {
				Label:          "Paris",
				SiteCount:      7,
				ByType:         map[string]int{"friche industrielle": 5, "friche d'habitat": 2},
				ByStatus:       map[string]int{"friche avec projet": 7},
				TotalSurfaceM2: 132_500,
			},
			"13055": {Label: "Marseille", SiteCount: 3},
			// A commune present in the index but with no site at all: the
			// Source must treat it exactly like a miss.
			"01001": {Label: "Vide", SiteCount: 0},
		},
	}
	src := NewSource(Options{Index: idx})

	t.Run("populated", func(t *testing.T) {
		r, err := src.QueryResult(context.Background(), listing("75116"))
		if err != nil {
			t.Fatalf("QueryResult: %v", err)
		}
		if r.IsEmpty() || r.SiteCount != 7 {
			t.Fatalf("SiteCount = %d, want 7", r.SiteCount)
		}
		if r.TotalSurfaceM2 != 132_500 {
			t.Errorf("TotalSurfaceM2 = %d m², want 132500", r.TotalSurfaceM2)
		}
		if r.Confidence != ConfidenceHigh {
			t.Errorf("Confidence = %q, want %q", r.Confidence, ConfidenceHigh)
		}
		if r.ByType["friche industrielle"] != 5 || r.ByStatus["friche avec projet"] != 7 {
			t.Errorf("breakdowns = %v / %v", r.ByType, r.ByStatus)
		}
		ev := r.Evidence
		if ev.INSEE != "75056" {
			t.Errorf("Evidence.INSEE = %q, want the folded 75056", ev.INSEE)
		}
		if ev.CommuneLabel != "Paris" {
			t.Errorf("Evidence.CommuneLabel = %q, want Paris", ev.CommuneLabel)
		}
		if ev.RowCountCommunes != idx.Count() {
			t.Errorf("Evidence.RowCountCommunes = %d, want %d", ev.RowCountCommunes, idx.Count())
		}
		if ev.RowCountSites != 41_234 {
			t.Errorf("Evidence.RowCountSites = %d, want 41234", ev.RowCountSites)
		}
	})

	t.Run("folding", func(t *testing.T) {
		for _, in := range []string{"75101", "75120", "69382", "13208"} {
			r, err := src.QueryResult(context.Background(), listing(in))
			if err != nil {
				t.Fatalf("%s: %v", in, err)
			}
			switch in[:2] {
			case "75":
				if r.Evidence.INSEE != "75056" || r.SiteCount != 7 {
					t.Errorf("%s folded to %q (%d sites), want 75056 (7)", in, r.Evidence.INSEE, r.SiteCount)
				}
			case "69":
				if r.Evidence.INSEE != "69123" {
					t.Errorf("%s folded to %q, want 69123", in, r.Evidence.INSEE)
				}
			case "13":
				if r.Evidence.INSEE != "13055" || r.SiteCount != 3 {
					t.Errorf("%s folded to %q (%d sites), want 13055 (3)", in, r.Evidence.INSEE, r.SiteCount)
				}
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		for _, insee := range []string{"99999", "01001"} { // absent, then present-but-zero
			r, err := src.QueryResult(context.Background(), listing(insee))
			if err != nil {
				t.Fatalf("%s: %v", insee, err)
			}
			if !r.IsEmpty() {
				t.Errorf("%s: IsEmpty = false, want true", insee)
			}
			if r.Confidence != ConfidenceNone {
				t.Errorf("%s: Confidence = %q, want empty", insee, r.Confidence)
			}
			if r.ByType != nil || r.ByStatus != nil || r.TotalSurfaceM2 != 0 {
				t.Errorf("%s: an empty Result must carry no breakdown: %+v", insee, r)
			}
			// Evidence still says what was consulted.
			if r.Evidence.INSEE == "" || r.Evidence.RowCountSites != 41_234 {
				t.Errorf("%s: Evidence = %+v, want the consulted-dataset stamps", insee, r.Evidence)
			}
			if r.Evidence.CommuneLabel != "" {
				t.Errorf("%s: Evidence.CommuneLabel = %q, want empty on a miss", insee, r.Evidence.CommuneLabel)
			}
		}
	})
}

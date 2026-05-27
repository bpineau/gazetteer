package zonageabc

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 30000 {
		t.Errorf("Count = %d, want >= 30000", got)
	}
	if idx.Meta.EffectiveDate == "" {
		t.Errorf("EffectiveDate empty")
	}
}

// TestQuery_GoldenCases pins zone classification for well-known
// communes.
func TestQuery_GoldenCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		insee    string
		want     Zone
		wantFold bool   // true if Evidence.LookupINSEE should be populated
		foldedTo string // expected parent commune if wantFold
	}{
		{"paris-arr-folded-to-parent-Abis", "75111", ZoneAbis, true, "75056"},
		{"paris-parent-Abis", "75056", ZoneAbis, false, ""},
		{"lyon-arr-folded-A", "69383", ZoneA, true, "69123"},
		{"marseille-arr-folded", "13208", ZoneA, true, "13055"},
		{"bordeaux-A", "33063", ZoneA, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := gazetteer.Listing{INSEE: c.insee}
			res, err := Query(context.Background(), Options{}, l)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if res == nil || res.IsEmpty() {
				t.Fatalf("empty result for %s", c.insee)
			}
			if res.Zone != c.want {
				t.Errorf("Zone = %q, want %q", res.Zone, c.want)
			}
			if res.Confidence != ConfidenceHigh {
				t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
			}
			if res.TensionScore != TensionScore(c.want) {
				t.Errorf("TensionScore = %d, want %d", res.TensionScore, TensionScore(c.want))
			}
			if res.Evidence.INSEE != c.insee {
				t.Errorf("Evidence.INSEE = %q, want %q", res.Evidence.INSEE, c.insee)
			}
			if c.wantFold && res.Evidence.LookupINSEE != c.foldedTo {
				t.Errorf("Evidence.LookupINSEE = %q, want %q", res.Evidence.LookupINSEE, c.foldedTo)
			}
			if !c.wantFold && res.Evidence.LookupINSEE != "" {
				t.Errorf("Evidence.LookupINSEE = %q, want empty (no folding)", res.Evidence.LookupINSEE)
			}
			if res.Evidence.EffectiveDate == "" {
				t.Errorf("Evidence.EffectiveDate empty")
			}
		})
	}
}

// TestQuery_UnknownCommune returns an empty result with a
// none-confidence flag.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "99999"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true for synthetic INSEE")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceNone)
	}
	if res.TensionScore != -1 {
		t.Errorf("TensionScore = %d, want -1", res.TensionScore)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestSource_NameVersion pins the canonical identifier + version.
func TestSource_NameVersion(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

// TestFrom_RoundtripFromDossier validates the gazetteer Register hook.
func TestFrom_RoundtripFromDossier(t *testing.T) {
	t.Parallel()
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, expected init() to register", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}

// TestTensionScore pins the ordinal mapping.
func TestTensionScore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		zone Zone
		want int
	}{
		{ZoneAbis, 4},
		{ZoneA, 3},
		{ZoneB1, 2},
		{ZoneB2, 1},
		{ZoneC, 0},
		{ZoneUnknown, -1},
		{Zone("garbage"), -1},
	}
	for _, c := range cases {
		if got := TensionScore(c.zone); got != c.want {
			t.Errorf("TensionScore(%q) = %d, want %d", c.zone, got, c.want)
		}
	}
}

// TestStubIndex exercises the Options.Index injection path used by
// downstream tests that want to avoid the embedded JSON.
func TestStubIndex(t *testing.T) {
	t.Parallel()
	stub := &Index{
		Meta: Meta{EffectiveDate: "2099-01-01"},
		Communes: map[string]Zone{
			"12345": ZoneB1,
		},
	}
	res, err := Query(context.Background(), Options{Index: stub}, gazetteer.Listing{INSEE: "12345"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Zone != ZoneB1 {
		t.Errorf("Zone = %q, want %q", res.Zone, ZoneB1)
	}
	if res.Evidence.EffectiveDate != "2099-01-01" {
		t.Errorf("EffectiveDate = %q, want injected", res.Evidence.EffectiveDate)
	}
}

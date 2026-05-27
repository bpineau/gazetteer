package zonetendue

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
	if got := idx.CountTendue(); got < 1000 {
		t.Errorf("CountTendue = %d, want >= 1000", got)
	}
	if idx.Meta.EffectiveDate == "" {
		t.Errorf("EffectiveDate empty")
	}
}

// TestQuery_GoldenCases pins classification for well-known communes.
func TestQuery_GoldenCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		insee     string
		wantTier  Tier
		wantTLV13 bool
	}{
		// Paris itself appears in the dataset (75056). The 2013 décret
		// lists Paris; the 2025 revision keeps it.
		{"paris-tendue", "75056", TierTendue, true},
		// Bordeaux (33063): tendue per the 2013 + 2025 décrets.
		{"bordeaux-tendue", "33063", TierTendue, true},
		// A commune outside any tendue zonage (rural commune in dept 03).
		{"rural-non-tendue", "03001", TierNonTendue, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := gazetteer.Listing{INSEE: c.insee}
			res, err := Query(context.Background(), Options{}, l)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if res == nil {
				t.Fatalf("nil result")
			}
			if res.Tier != c.wantTier {
				t.Errorf("Tier = %q, want %q", res.Tier, c.wantTier)
			}
			if res.IsTendue != (c.wantTier == TierTendue || c.wantTier == TierTendueTouristique) {
				t.Errorf("IsTendue = %v, want derived-from-tier", res.IsTendue)
			}
			if res.FlaggedTLV2013 != c.wantTLV13 {
				t.Errorf("FlaggedTLV2013 = %v, want %v", res.FlaggedTLV2013, c.wantTLV13)
			}
			if res.Confidence != ConfidenceHigh {
				t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
			}
			if res.Evidence.INSEE != c.insee {
				t.Errorf("Evidence.INSEE = %q, want %q", res.Evidence.INSEE, c.insee)
			}
			if res.Evidence.EffectiveDate == "" {
				t.Errorf("Evidence.EffectiveDate empty")
			}
		})
	}
}

// TestQuery_TouristiqueTier validates the second tier surfaces.
func TestQuery_TouristiqueTier(t *testing.T) {
	t.Parallel()
	// Inject a stub to pin a touristique row deterministically (the
	// real-data communes that fall in this tier could be re-classified
	// in future revisions; the stub keeps the test stable).
	stub := &Index{
		Meta: Meta{EffectiveDate: "2099-01-01"},
		Communes: map[string]Entry{
			"99000": {Tier: TierTendueTouristique, TLV2013: false},
		},
	}
	res, err := Query(context.Background(), Options{Index: stub}, gazetteer.Listing{INSEE: "99000"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Tier != TierTendueTouristique {
		t.Errorf("Tier = %q, want %q", res.Tier, TierTendueTouristique)
	}
	if !res.IsTendue {
		t.Errorf("IsTendue = false, want true for touristique tier")
	}
	if res.FlaggedTLV2013 {
		t.Errorf("FlaggedTLV2013 = true, want false")
	}
}

// TestQuery_NonTendueAbsence: communes absent from the index produce
// a non_tendue Result with high confidence.
func TestQuery_NonTendueAbsence(t *testing.T) {
	t.Parallel()
	// Synthetic INSEE — guaranteed to be absent from the dataset.
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Tier != TierNonTendue {
		t.Errorf("Tier = %q, want %q", res.Tier, TierNonTendue)
	}
	if res.IsTendue {
		t.Errorf("IsTendue = true, want false")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.IsEmpty() {
		t.Errorf("IsEmpty = true, want false — non_tendue is a populated answer")
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

// TestResultIsEmpty pins the IsEmpty semantics — only structurally
// empty Results report true.
func TestResultIsEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    *Result
		want bool
	}{
		{"nil", nil, true},
		{"zero", &Result{}, true},
		{"non-tendue-populated", &Result{Tier: TierNonTendue}, false},
		{"tendue", &Result{Tier: TierTendue, IsTendue: true}, false},
	}
	for _, c := range cases {
		if got := c.r.IsEmpty(); got != c.want {
			t.Errorf("%s: IsEmpty = %v, want %v", c.name, got, c.want)
		}
	}
}

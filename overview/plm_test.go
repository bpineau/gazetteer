package overview

import (
	"context"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

// TestBuild_PLMArrondissementsMatchTheSources pins that the batch join and
// the per-address Sources agree on every Paris, Lyon and Marseille
// arrondissement: the zonage datasets carry the parent commune only, and the
// batch path must fold the arrondissement onto it as the Sources do.
func TestBuild_PLMArrondissementsMatchTheSources(t *testing.T) {
	t.Parallel()
	rows, err := Build(Options{Depts: []string{"75", "69", "13"}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	seen := 0
	for _, r := range rows {
		if !strings.HasPrefix(r.INSEE, "751") && !strings.HasPrefix(r.INSEE, "6938") && !strings.HasPrefix(r.INSEE, "132") {
			continue
		}
		seen++
		l := gazetteer.Listing{INSEE: r.INSEE}
		abc, err := zonageabc.Query(context.Background(), zonageabc.Options{}, l)
		if err != nil {
			t.Fatalf("zonageabc %s: %v", r.INSEE, err)
		}
		if r.ZonageABC == "" || r.ZonageABC != string(abc.Zone) {
			t.Errorf("%s: overview ZonageABC %q, source %q", r.INSEE, r.ZonageABC, abc.Zone)
		}
		zt, err := zonetendue.Query(context.Background(), zonetendue.Options{}, l)
		if err != nil {
			t.Fatalf("zonetendue %s: %v", r.INSEE, err)
		}
		if r.ZoneTendue == "" || r.ZoneTendue != string(zt.Tier) {
			t.Errorf("%s: overview ZoneTendue %q, source %q", r.INSEE, r.ZoneTendue, zt.Tier)
		}
	}
	if seen != 45 {
		t.Errorf("checked %d arrondissements, want 45 (20 Paris + 9 Lyon + 16 Marseille)", seen)
	}
}

package sitadel

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestEmbeddedArtifact loads the committed embedded dataset and asserts a
// known commune (Saint-Denis 93066) returns sane values. It exercises the real
// gzip+json artifact, not a stub.
func TestEmbeddedArtifact(t *testing.T) {
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx.Count() < 30000 {
		t.Fatalf("embedded artifact has only %d communes, expected national coverage", idx.Count())
	}
	if !millesimeRE.MatchString(idx.Meta.DataMillesime) {
		t.Errorf("DataMillesime = %q, want a YYYY-MM millésime", idx.Meta.DataMillesime)
	}

	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "93066"})
	if err != nil {
		t.Fatalf("Query 93066: %v", err)
	}
	if r.IsEmpty() {
		t.Fatalf("93066 Saint-Denis unexpectedly empty")
	}
	// Confirmed from the upstream 2026-09 millésime: 2025 Tous Logements
	// LOG_AUT=515 with LOG_COM blank (the provisional final year), 2024
	// LOG_COM=440. Starts are revised as late declarations arrive, so a
	// later millésime may move these.
	if r.LatestYear != 2025 || r.AuthorizedLatest != 515 {
		t.Errorf("93066 AuthorizedLatest=%d (%d), want 515 (2025)", r.AuthorizedLatest, r.LatestYear)
	}
	if r.StartedLatestYear != 2024 || r.StartedLatest != 440 {
		t.Errorf("93066 StartedLatest=%d (%d), want 440 (2024)", r.StartedLatest, r.StartedLatestYear)
	}
	if len(r.AuthorizedSeries) == 0 {
		t.Errorf("93066 expected a non-empty authorized series")
	}

	// Paris (folds 75118 -> 75056) must resolve via the aggregate row.
	rp, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("Query 75118: %v", err)
	}
	if rp.IsEmpty() || rp.Evidence.INSEE != "75056" {
		t.Errorf("Paris fold failed: empty=%v insee=%q", rp.IsEmpty(), rp.Evidence.INSEE)
	}
}

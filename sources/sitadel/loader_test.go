package sitadel

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestEmbeddedArtifact loads the committed embedded dataset and asserts a
// known commune (Achères 78005) returns sane values. It exercises the real
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

	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "78005"})
	if err != nil {
		t.Fatalf("Query 78005: %v", err)
	}
	if r.IsEmpty() {
		t.Fatalf("78005 Achères unexpectedly empty")
	}
	// Confirmed from the upstream 2026-09 millésime: 2024 Tous Logements
	// LOG_AUT=3, LOG_COM=271 (71 in the 2026-06 millésime: starts are
	// revised as late declarations arrive); 2025 LOG_AUT=6 with LOG_COM blank.
	if r.LatestYear != 2025 || r.AuthorizedLatest != 6 {
		t.Errorf("78005 AuthorizedLatest=%d (%d), want 6 (2025)", r.AuthorizedLatest, r.LatestYear)
	}
	if r.StartedLatestYear != 2024 || r.StartedLatest != 271 {
		t.Errorf("78005 StartedLatest=%d (%d), want 271 (2024)", r.StartedLatest, r.StartedLatestYear)
	}
	if len(r.AuthorizedSeries) == 0 {
		t.Errorf("78005 expected a non-empty authorized series")
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

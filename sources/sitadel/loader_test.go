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
	if idx.Meta.DataMillesime != dataMillesime {
		t.Errorf("DataMillesime = %q, want %q", idx.Meta.DataMillesime, dataMillesime)
	}

	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "78005"})
	if err != nil {
		t.Fatalf("Query 78005: %v", err)
	}
	if r.IsEmpty() {
		t.Fatalf("78005 Achères unexpectedly empty")
	}
	// Confirmed from the upstream file: 2024 Tous Logements LOG_AUT=3,
	// LOG_COM=71; 2025 LOG_AUT=6 with LOG_COM blank.
	if r.LatestYear != 2025 || r.AuthorizedLatest != 6 {
		t.Errorf("78005 AuthorizedLatest=%d (%d), want 6 (2025)", r.AuthorizedLatest, r.LatestYear)
	}
	if r.StartedLatestYear != 2024 || r.StartedLatest != 71 {
		t.Errorf("78005 StartedLatest=%d (%d), want 71 (2024)", r.StartedLatest, r.StartedLatestYear)
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

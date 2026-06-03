package sitadel

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestQueryInsufficientInputs(t *testing.T) {
	_, err := Query(context.Background(), Options{Index: &Index{}}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

func TestQueryNoMatchEmpty(t *testing.T) {
	idx := &Index{Meta: Meta{DataMillesime: "2026-06"}, Communes: map[string]Entry{}}
	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "12345"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !r.IsEmpty() {
		t.Fatalf("expected IsEmpty for absent commune, got %+v", r)
	}
	if r.Evidence.DataMillesime != "2026-06" {
		t.Errorf("Evidence.DataMillesime = %q", r.Evidence.DataMillesime)
	}
}

func TestQueryProjection(t *testing.T) {
	// 2020..2025, Started blank for 2025 (provisional), like Achères.
	idx := &Index{
		Meta: Meta{DataMillesime: "2026-06"},
		Communes: map[string]Entry{
			"78005": {
				YearStart: 2020,
				Auth:      []int{10, 12, 20, 5, 3, 6},
				Started:   []int{8, 9, 15, 30, 71, missing},
				CollAuth:  []int{6, 8, 14, 4, 2, 5},
			},
		},
	}
	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "78005"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.IsEmpty() {
		t.Fatalf("expected populated result")
	}
	if r.LatestYear != 2025 || r.AuthorizedLatest != 6 {
		t.Errorf("AuthorizedLatest=%d (year %d), want 6 (2025)", r.AuthorizedLatest, r.LatestYear)
	}
	if r.StartedLatestYear != 2024 || r.StartedLatest != 71 {
		t.Errorf("StartedLatest=%d (year %d), want 71 (2024)", r.StartedLatest, r.StartedLatestYear)
	}
	// Auth last 5: 12,20,5,3,6 -> mean 9.2
	if r.AuthorizedAvg5y != 9.2 {
		t.Errorf("AuthorizedAvg5y=%v, want 9.2", r.AuthorizedAvg5y)
	}
	// Started present last 5: 9,15,30,71 (2025 missing) -> over avgWindow scan
	// from end: skip 2025(missing), take 71,30,15,9,8 = 5 values -> mean 26.6
	if r.StartedAvg5y != 26.6 {
		t.Errorf("StartedAvg5y=%v, want 26.6", r.StartedAvg5y)
	}
	// Collectif share over last 5 auth years (2021..2025):
	// coll 8+14+4+2+5=33 over auth 12+20+5+3+6=46 -> 71.7%
	if r.CollectifSharePct != 71.7 {
		t.Errorf("CollectifSharePct=%v, want 71.7", r.CollectifSharePct)
	}
	want := []int{10, 12, 20, 5, 3, 6}
	if !equalInts(r.AuthorizedSeries, want) || r.SeriesStartYear != 2020 {
		t.Errorf("AuthorizedSeries=%v (start %d), want %v (2020)", r.AuthorizedSeries, r.SeriesStartYear, want)
	}
	if r.Evidence.RowYears != 6 {
		t.Errorf("RowYears=%d, want 6", r.Evidence.RowYears)
	}
}

func TestQueryFoldsArrondissement(t *testing.T) {
	idx := &Index{
		Communes: map[string]Entry{
			"75056": {YearStart: 2024, Auth: []int{3924}, Started: []int{1298}, CollAuth: []int{3800}},
		},
	}
	// A Paris arrondissement INSEE must fold to 75056 and hit.
	r, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.IsEmpty() || r.AuthorizedLatest != 3924 {
		t.Fatalf("expected fold 75118->75056, got %+v", r)
	}
	if r.Evidence.INSEE != "75056" {
		t.Errorf("Evidence.INSEE=%q, want folded 75056", r.Evidence.INSEE)
	}
}

func TestEmptyEntryIsEmpty(t *testing.T) {
	// An entry whose only authorised value is 0 should not exist in the
	// index (transform drops it), but project must also be safe.
	r := project(Entry{YearStart: 2024, Auth: []int{0}, Started: []int{missing}, CollAuth: []int{0}})
	if !r.IsEmpty() {
		t.Errorf("zero-authorised entry should project to IsEmpty")
	}
}

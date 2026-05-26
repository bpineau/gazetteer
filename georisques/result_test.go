package georisques

import (
	"reflect"
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
)

// Compile-time check: *Result satisfies appraisal.HazardReporter.
var _ appraisal.HazardReporter = (*Result)(nil)

func TestResult_HazardReportMapping(t *testing.T) {
	t.Parallel()

	r := &Result{
		Naturels: map[string]RiskBlob{
			"inondation":     {Present: true},
			"seisme":         {Present: true},
			"remontee_nappe": {Present: false}, // excluded
			"feu_foret":      {Present: true},
		},
		Technos: map[string]RiskBlob{
			"icpe":           {Present: true},
			"nucleaire":      {Present: false}, // excluded
			"pollution_sols": {Present: true},
		},
	}
	got := r.HazardReport()

	wantNatural := []string{"feu_foret", "inondation", "seisme"}
	wantIndustrial := []string{"icpe", "pollution_sols"}
	if !reflect.DeepEqual(got.NaturalRisks, wantNatural) {
		t.Errorf("NaturalRisks = %v, want %v", got.NaturalRisks, wantNatural)
	}
	if !reflect.DeepEqual(got.IndustrialRisks, wantIndustrial) {
		t.Errorf("IndustrialRisks = %v, want %v", got.IndustrialRisks, wantIndustrial)
	}
	if got.Confidence != appraisal.ConfidenceHigh {
		t.Errorf("Confidence = %v, want High", got.Confidence)
	}
}

func TestResult_HazardReportEmpty(t *testing.T) {
	t.Parallel()

	// All risks absent → empty slices, still High confidence (the report
	// itself is canonical state data; absence is reliable info).
	r := &Result{
		Naturels: map[string]RiskBlob{
			"inondation": {Present: false},
			"seisme":     {Present: false},
		},
		Technos: map[string]RiskBlob{},
	}
	got := r.HazardReport()
	if len(got.NaturalRisks) != 0 {
		t.Errorf("NaturalRisks = %v, want empty", got.NaturalRisks)
	}
	if len(got.IndustrialRisks) != 0 {
		t.Errorf("IndustrialRisks = %v, want empty", got.IndustrialRisks)
	}
	if got.Confidence != appraisal.ConfidenceHigh {
		t.Errorf("Confidence = %v, want High", got.Confidence)
	}
}

func TestResult_HazardReportNilSafe(t *testing.T) {
	t.Parallel()

	var r *Result
	got := r.HazardReport()
	if len(got.NaturalRisks) != 0 || len(got.IndustrialRisks) != 0 {
		t.Errorf("nil receiver should yield empty HazardReport, got %+v", got)
	}
}

func TestResult_HazardReportDeterministicOrder(t *testing.T) {
	t.Parallel()

	// Map iteration is unordered in Go — exercise the sort guarantee by
	// running the same input through HazardReport multiple times.
	r := &Result{
		Naturels: map[string]RiskBlob{
			"seisme":         {Present: true},
			"inondation":     {Present: true},
			"feu_foret":      {Present: true},
			"remontee_nappe": {Present: true},
		},
	}
	first := r.HazardReport().NaturalRisks
	for i := 0; i < 20; i++ {
		got := r.HazardReport().NaturalRisks
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("HazardReport order drift at iteration %d: %v vs %v", i, got, first)
		}
	}
	want := []string{"feu_foret", "inondation", "remontee_nappe", "seisme"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("NaturalRisks = %v, want %v (sorted)", first, want)
	}
}

package appraisal

import (
	"reflect"
	"testing"

	"github.com/bpineau/gazetteer"
)

// fakeHazardReporter is a minimal Result.Data type that satisfies
// HazardReporter. Used by the tests so they don't depend on real Sources.
type fakeHazardReporter struct {
	natural    []string
	industrial []string
	confidence Confidence
}

func (f fakeHazardReporter) HazardReport() HazardReport {
	return HazardReport{
		NaturalRisks:    f.natural,
		IndustrialRisks: f.industrial,
		Confidence:      f.confidence,
	}
}

// nonHazardData satisfies neither HazardReporter nor anything else.
type nonHazardData struct{}

// Compile-time check: fakeHazardReporter must satisfy HazardReporter.
var _ HazardReporter = fakeHazardReporter{}

func TestHazardProfile_SingleSource(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"georisques": {data: fakeHazardReporter{
			natural:    []string{"inondation", "seisme"},
			industrial: []string{"icpe"},
			confidence: ConfidenceHigh,
		}},
	})

	got := HazardProfile(d)
	if !reflect.DeepEqual(got.NaturalRisks, []string{"inondation", "seisme"}) {
		t.Errorf("NaturalRisks = %v, want [inondation seisme]", got.NaturalRisks)
	}
	if !reflect.DeepEqual(got.IndustrialRisks, []string{"icpe"}) {
		t.Errorf("IndustrialRisks = %v, want [icpe]", got.IndustrialRisks)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low (single source)", got.Confidence)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Source != "georisques" {
		t.Errorf("Inputs = %+v, want one entry for georisques", got.Inputs)
	}
}

func TestHazardProfile_TwoSourcesUnion(t *testing.T) {
	t.Parallel()

	// Two sources contributing distinct + overlapping risks → set union,
	// no duplicates, Confidence Medium.
	d := buildDossier(map[string]fakeEntry{
		"georisques": {data: fakeHazardReporter{
			natural:    []string{"inondation", "seisme"},
			industrial: []string{"icpe"},
		}},
		"hazardlite": {data: fakeHazardReporter{
			natural:    []string{"seisme", "feux_foret"}, // "seisme" dup with georisques
			industrial: []string{"seveso"},
		}},
	})

	got := HazardProfile(d)
	wantNatural := []string{"feux_foret", "inondation", "seisme"}
	wantIndustrial := []string{"icpe", "seveso"}
	if !reflect.DeepEqual(got.NaturalRisks, wantNatural) {
		t.Errorf("NaturalRisks = %v, want %v", got.NaturalRisks, wantNatural)
	}
	if !reflect.DeepEqual(got.IndustrialRisks, wantIndustrial) {
		t.Errorf("IndustrialRisks = %v, want %v", got.IndustrialRisks, wantIndustrial)
	}
	if got.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %v, want Medium (2 sources)", got.Confidence)
	}
	if len(got.Inputs) != 2 {
		t.Errorf("Inputs len = %d, want 2", len(got.Inputs))
	}
}

func TestHazardProfile_ThreeSourcesConfidenceHigh(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"src_a": {data: fakeHazardReporter{natural: []string{"inondation"}}},
		"src_b": {data: fakeHazardReporter{natural: []string{"seisme"}}},
		"src_c": {data: fakeHazardReporter{industrial: []string{"icpe"}}},
	})

	got := HazardProfile(d)
	if got.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %v, want High (3 sources)", got.Confidence)
	}
	if len(got.Inputs) != 3 {
		t.Errorf("Inputs len = %d, want 3", len(got.Inputs))
	}
}

func TestHazardProfile_NoImplementers(t *testing.T) {
	t.Parallel()

	// Sources present but none satisfy HazardReporter — empty consolidated.
	d := buildDossier(map[string]fakeEntry{
		"dvf":   {data: nonHazardData{}},
		"ademe": {data: nonHazardData{}},
	})

	got := HazardProfile(d)
	if len(got.NaturalRisks) != 0 {
		t.Errorf("NaturalRisks = %v, want empty", got.NaturalRisks)
	}
	if len(got.IndustrialRisks) != 0 {
		t.Errorf("IndustrialRisks = %v, want empty", got.IndustrialRisks)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
	if len(got.Inputs) != 0 {
		t.Errorf("Inputs len = %d, want 0", len(got.Inputs))
	}
}

func TestHazardProfile_FailedSourcesSkipped(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"georisques": {
			data:   fakeHazardReporter{natural: []string{"inondation"}},
			status: gazetteer.StatusOK,
		},
		"broken": {
			data:   fakeHazardReporter{natural: []string{"seisme"}},
			status: gazetteer.StatusFailedAntiBot,
		},
		"transient": {
			data:   fakeHazardReporter{natural: []string{"feux_foret"}},
			status: gazetteer.StatusFailedTransient,
		},
	})

	got := HazardProfile(d)
	if !reflect.DeepEqual(got.NaturalRisks, []string{"inondation"}) {
		t.Errorf("NaturalRisks = %v, want [inondation] (failed sources skipped)", got.NaturalRisks)
	}
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1 (only OK source kept)", len(got.Inputs))
	}
}

func TestHazardProfile_DuplicateRisksDeduplicated(t *testing.T) {
	t.Parallel()

	// Three sources all reporting "inondation" → consolidated has it once.
	d := buildDossier(map[string]fakeEntry{
		"src_a": {data: fakeHazardReporter{natural: []string{"inondation", "seisme"}}},
		"src_b": {data: fakeHazardReporter{natural: []string{"inondation"}}},
		"src_c": {data: fakeHazardReporter{natural: []string{"inondation", "seisme"}}},
	})

	got := HazardProfile(d)
	want := []string{"inondation", "seisme"}
	if !reflect.DeepEqual(got.NaturalRisks, want) {
		t.Errorf("NaturalRisks = %v, want %v (deduped)", got.NaturalRisks, want)
	}
}

func TestHazardProfile_EmptyDossier(t *testing.T) {
	t.Parallel()

	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got := HazardProfile(d)
	if len(got.NaturalRisks) != 0 || len(got.IndustrialRisks) != 0 {
		t.Errorf("got non-empty consolidated on empty dossier: %+v", got)
	}
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", got.Confidence)
	}
}

func TestHazardProfile_StatusOKEmptyKept(t *testing.T) {
	t.Parallel()

	// StatusOKEmpty still implements HazardReporter — kept (the typed
	// payload decides whether it has risks to report).
	d := buildDossier(map[string]fakeEntry{
		"georisques": {
			data:   fakeHazardReporter{natural: []string{"inondation"}},
			status: gazetteer.StatusOKEmpty,
		},
	})

	got := HazardProfile(d)
	if len(got.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1 (StatusOKEmpty kept)", len(got.Inputs))
	}
	if !reflect.DeepEqual(got.NaturalRisks, []string{"inondation"}) {
		t.Errorf("NaturalRisks = %v, want [inondation]", got.NaturalRisks)
	}
}

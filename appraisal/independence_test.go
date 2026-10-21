package appraisal

import "testing"

// fakeDatasetEstimator is a PriceEstimator that declares the dataset its
// reading is cut from, the way dvf and dvfagg both declare "dvf".
type fakeDatasetEstimator struct {
	cents   int64
	conf    Confidence
	dataset string
}

func (f fakeDatasetEstimator) PriceEstimate() PriceEstimate {
	return PriceEstimate{EurPerM2Cents: f.cents, Confidence: f.conf, Dataset: f.dataset}
}

// Two contributors cut from ONE dataset are one source of truth: dvf (the
// mutations near the address) and dvfagg (the same mutations, aggregated per
// commune) cannot corroborate each other, so together they must not clear
// MinSources=2 and report ConfidenceHigh.
func TestPricePerM2_SameDatasetCountsOnce(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"dvf":    {data: fakeDatasetEstimator{cents: 5_000_00, conf: ConfidenceHigh, dataset: "dvf"}},
		"dvfagg": {data: fakeDatasetEstimator{cents: 5_200_00, conf: ConfidenceHigh, dataset: "dvf"}},
	})
	got := PricePerM2(d)
	if got.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want low: dvf + dvfagg are one dataset, below MinSources=2", got.Confidence)
	}
	if got.EurPerM2Cents == 0 {
		t.Error("the value must still be computed")
	}

	// A genuinely independent second dataset does clear the floor.
	d.Results["meilleursagents"] = buildDossier(map[string]fakeEntry{
		"meilleursagents": {data: fakeDatasetEstimator{cents: 5_100_00, conf: ConfidenceHigh}},
	}).Results["meilleursagents"]
	if got := PricePerM2(d); got.Confidence != ConfidenceHigh {
		t.Errorf("with an independent third contributor Confidence = %v, want high", got.Confidence)
	}
}

// An estimator that declares no dataset is its own: the historical behaviour.
func TestPricePerM2_EmptyDatasetIsIndependent(t *testing.T) {
	t.Parallel()

	d := buildDossier(map[string]fakeEntry{
		"a": {data: fakeDatasetEstimator{cents: 5_000_00, conf: ConfidenceHigh}},
		"b": {data: fakeDatasetEstimator{cents: 5_000_00, conf: ConfidenceHigh}},
	})
	if got := PricePerM2(d); got.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %v, want high (two undeclared contributors are two datasets)", got.Confidence)
	}
}

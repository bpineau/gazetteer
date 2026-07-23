package dvfagg_test

import (
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/dvfagg"
)

func TestPriceEstimate_ConfidenceTiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		r          dvfagg.Result
		wantCents  int64
		wantConf   appraisal.Confidence
		wantSample int
	}{
		{
			name:      "large tight sample → High",
			r:         dvfagg.Result{PriceMedianEURM2: 6000, PriceP25EURM2: 5000, PriceP75EURM2: 7000, N: 120},
			wantCents: 600000, wantConf: appraisal.ConfidenceHigh, wantSample: 120,
		},
		{
			name:      "large but bimodal sample → Medium (dispersion caps it)",
			r:         dvfagg.Result{PriceMedianEURM2: 4000, PriceP25EURM2: 2000, PriceP75EURM2: 5000, N: 120},
			wantCents: 400000, wantConf: appraisal.ConfidenceMedium, wantSample: 120,
		},
		{
			name:      "solid sample → Medium",
			r:         dvfagg.Result{PriceMedianEURM2: 3500, PriceP25EURM2: 3000, PriceP75EURM2: 4000, N: 20},
			wantCents: 350000, wantConf: appraisal.ConfidenceMedium, wantSample: 20,
		},
		{
			name:      "thin sample → Low",
			r:         dvfagg.Result{PriceMedianEURM2: 3500, PriceP25EURM2: 3000, PriceP75EURM2: 4000, N: 5},
			wantCents: 350000, wantConf: appraisal.ConfidenceLow, wantSample: 5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			est := c.r.PriceEstimate()
			if est.EurPerM2Cents != c.wantCents {
				t.Errorf("EurPerM2Cents = %d, want %d", est.EurPerM2Cents, c.wantCents)
			}
			if est.Confidence != c.wantConf {
				t.Errorf("Confidence = %v, want %v", est.Confidence, c.wantConf)
			}
			if est.SampleSize != c.wantSample {
				t.Errorf("SampleSize = %d, want %d", est.SampleSize, c.wantSample)
			}
		})
	}
}

// TestPricePerM2_DVFAndDVFAgg is the core of Item 4: with both a live dvf
// reading and the embedded dvfagg commune median, appraisal.PricePerM2 clears
// its MinSources=2 floor and reports Medium/High — no longer structurally Low.
func TestPricePerM2_DVFAndDVFAgg(t *testing.T) {
	t.Parallel()
	cents := int64(610000) // 6100 €/m²
	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{
		dvf.Name: {Name: dvf.Name, Status: gazetteer.StatusOK, Data: &dvf.Result{
			ValueEURPerM2Cents: &cents, SampleSize: 40, Confidence: "high",
		}},
		dvfagg.Name: {Name: dvfagg.Name, Status: gazetteer.StatusOK, Data: &dvfagg.Result{
			PriceMedianEURM2: 6000, PriceP25EURM2: 5000, PriceP75EURM2: 7000, N: 120,
		}},
	}}

	got := appraisal.PricePerM2(d)
	if got.Confidence == appraisal.ConfidenceLow {
		t.Fatalf("Confidence = Low, want Medium/High (dvf + dvfagg satisfy MinSources=2)")
	}
	if len(got.Inputs) != 2 {
		t.Errorf("Inputs = %d, want 2 (dvf + dvfagg)", len(got.Inputs))
	}
	// The consolidated €/m² sits between the two readings (6000–6100).
	if got.EurPerM2Cents < 600000 || got.EurPerM2Cents > 610000 {
		t.Errorf("EurPerM2Cents = %d, want within [600000, 610000]", got.EurPerM2Cents)
	}
}

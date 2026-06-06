package dvfagg

import "testing"

func TestResult_IsEmpty(t *testing.T) {
	if !(&Result{}).IsEmpty() {
		t.Fatal("zero Result must be empty")
	}
	if (*Result)(nil).IsEmpty() != true {
		t.Fatal("nil Result must be empty")
	}
	if (&Result{N: 5, PriceMedianEURM2: 2500}).IsEmpty() {
		t.Fatal("populated Result must not be empty")
	}
}

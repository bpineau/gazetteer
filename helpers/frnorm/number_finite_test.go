package frnorm

import "testing"

func TestParseFRFloat_NonFinite(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"NaN", "nan", "Inf", "-Inf", "+infinity", "1e999"} {
		if v, ok := ParseFRFloat(s); ok {
			t.Errorf("ParseFRFloat(%q) = %v, true; want ok=false", s, v)
		}
	}
	if v, ok := ParseFRFloat("1 234,5"); !ok || v != 1234.5 {
		t.Errorf("ParseFRFloat(1 234,5) = %v, %v", v, ok)
	}
}

package appraisal

import "testing"

func TestConfidence_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		c    Confidence
		want string
	}{
		{"low", ConfidenceLow, "low"},
		{"medium", ConfidenceMedium, "medium"},
		{"high", ConfidenceHigh, "high"},
		{"unknown_positive", Confidence(42), "unknown"},
		{"unknown_negative", Confidence(-1), "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.c.String(); got != tc.want {
				t.Errorf("Confidence(%d).String() = %q, want %q", int(tc.c), got, tc.want)
			}
		})
	}
}

func TestParseConfidence(t *testing.T) {
	cases := map[string]Confidence{
		"high":           ConfidenceHigh,
		"medium":         ConfidenceMedium,
		"low":            ConfidenceLow,
		"none":           ConfidenceLow,
		"commune_median": ConfidenceLow,
		"":               ConfidenceLow,
	}
	for in, want := range cases {
		if got := ParseConfidence(in); got != want {
			t.Errorf("ParseConfidence(%q) = %v, want %v", in, got, want)
		}
	}
	// Round-trips with String for the three canonical levels.
	for _, c := range []Confidence{ConfidenceLow, ConfidenceMedium, ConfidenceHigh} {
		if got := ParseConfidence(c.String()); got != c {
			t.Errorf("ParseConfidence(%v.String()) = %v, want identity", c, got)
		}
	}
}

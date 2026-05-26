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

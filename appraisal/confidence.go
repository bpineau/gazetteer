package appraisal

// Confidence is a coarse confidence indicator that consumers map to
// downstream UI labels or filter thresholds.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

// ParseConfidence maps a source-side confidence label onto the
// Confidence scale — the inverse of String. "high" and "medium" map to
// their levels; anything else (including "low", "none" and per-source
// labels like "commune_median") is ConfidenceLow, the conservative
// floor every estimator's hand-rolled switch already used.
func ParseConfidence(label string) Confidence {
	switch label {
	case "high":
		return ConfidenceHigh
	case "medium":
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// String returns a stable snake_case identifier suitable for logs and
// metrics labels.
func (c Confidence) String() string {
	switch c {
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	default:
		return "unknown"
	}
}

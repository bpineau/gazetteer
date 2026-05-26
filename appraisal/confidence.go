package appraisal

// Confidence is a coarse confidence indicator that consumers map to
// downstream UI labels or filter thresholds.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

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

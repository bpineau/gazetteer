package main

import (
	"testing"
	"time"
)

// TestDatasetFreshness is the freshness doctor: it walks every dataset Set the
// CLI knows and fails when one that declares a Vintage + cadence has gone stale
// (Overdue as of now). This is the guard that would have caught the encadrement
// barème and the dvfagg price window silently going years out of date —
// refresh the flagged source (and bump its Vintage) to clear it.
func TestDatasetFreshness(t *testing.T) {
	deps, err := newRuntimeDeps()
	if err != nil {
		t.Fatalf("newRuntimeDeps: %v", err)
	}
	bySource, names := collectDatasetSources(deps)

	now := time.Now()
	tracked := 0
	for _, name := range names {
		for _, s := range bySource[name] {
			if s.Vintage == "" {
				continue // untracked — freshness not asserted for this Set
			}
			tracked++
			if _, ok := s.VintageTime(); !ok {
				t.Errorf("%s / %s: malformed Vintage %q (want YYYY-MM)", name, s.Processed.Name, s.Vintage)
			}
			if s.Overdue(now) {
				t.Errorf("%s / %s: dataset OVERDUE (vintage %s, cadence %d mo) — refresh it and bump Vintage",
					name, s.Processed.Name, s.Vintage, s.ExpectedCadenceMonths)
			}
		}
	}
	if tracked == 0 {
		t.Error("no dataset declares a Vintage — the freshness guard is inert")
	}
}

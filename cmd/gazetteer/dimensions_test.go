package main

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestDimensionsComplete enforces the discovery-by-intent map can't drift:
// every registered source belongs to exactly one dimension, and no dimension
// lists an unregistered source.
func TestDimensionsComplete(t *testing.T) {
	registered := make(map[string]bool)
	for _, name := range gazetteer.RegisteredNames() {
		registered[name] = true
	}

	seen := make(map[string]int)
	for _, g := range sourceDimensions {
		if g.Dimension == "" {
			t.Error("a dimension has an empty name")
		}
		for _, s := range g.Sources {
			seen[s]++
			if !registered[s] {
				t.Errorf("dimension %q lists unregistered source %q", g.Dimension, s)
			}
		}
	}
	for name := range registered {
		switch seen[name] {
		case 0:
			t.Errorf("registered source %q is in no dimension — add it in dimensions.go", name)
		case 1:
		default:
			t.Errorf("source %q is in %d dimensions — each belongs to exactly one", name, seen[name])
		}
	}
}

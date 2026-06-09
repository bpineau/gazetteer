package roster

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// TestRosterCompleteness pins the roster to the source registry: every
// gazetteer.Register'ed source has exactly one Entry and vice versa, so
// the factory and the CLI (which both consume Entries) can never drift
// from the set of in-tree sources.
func TestRosterCompleteness(t *testing.T) {
	registered := map[string]bool{}
	for _, n := range gazetteer.RegisteredNames() {
		registered[n] = true
	}

	seen := map[string]bool{}
	for _, e := range Entries() {
		if seen[e.Name] {
			t.Errorf("duplicate roster entry %q", e.Name)
		}
		seen[e.Name] = true
		if !registered[e.Name] {
			t.Errorf("roster entry %q is not a registered source", e.Name)
		}
	}
	for n := range registered {
		if !seen[n] {
			t.Errorf("registered source %q has no roster entry", n)
		}
	}
}

// TestRosterBuildAll exercises every constructor with realistic deps —
// a wiring typo (wrong Options field, nil-dep panic) fails here instead
// of at first CLI/factory use.
func TestRosterBuildAll(t *testing.T) {
	hc, err := NewHTTPClient()
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	deps := Deps{
		HTTP:     hc,
		Geocoder: NewGeocoder(hc),
		Communes: communes.MustDefault(),
		DataDir:  "", // embedded-only
	}
	for _, e := range Entries() {
		src, err := e.Build(deps)
		if err != nil {
			t.Errorf("Build(%q): %v", e.Name, err)
			continue
		}
		if src == nil {
			t.Errorf("Build(%q) returned a nil Source", e.Name)
			continue
		}
		if src.Name() != e.Name {
			t.Errorf("Build(%q) built a Source named %q", e.Name, src.Name())
		}
	}
}

// TestRosterCLIOptIn pins the opt-in set: widening it silently changes
// what a default CLI run queries, so make that an explicit test edit.
func TestRosterCLIOptIn(t *testing.T) {
	var optIn []string
	for _, e := range Entries() {
		if e.CLIOptIn {
			optIn = append(optIn, e.Name)
		}
	}
	if len(optIn) != 1 || optIn[0] != "bdnb" {
		t.Errorf("CLIOptIn set = %v, want [bdnb]", optIn)
	}
}

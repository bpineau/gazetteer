package roster

import (
	"context"
	"reflect"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
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
		Geocoder: banx.NewDefaultGeocoder(hc),
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

// TestRosterQueryResultConvention pins the per-package typed-Query
// convention: every source exposes a QueryResult method with signature
// `func (s *Source) QueryResult(context.Context, gazetteer.Listing)
// (*Result, error)` — the typed counterpart of gazetteer.Source.Query
// for callers holding a long-lived Source instance. A future source
// without it (or with a drifting signature) fails here instead of at a
// consumer's call site.
func TestRosterQueryResultConvention(t *testing.T) {
	hc, err := NewHTTPClient()
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	deps := Deps{
		HTTP:     hc,
		Geocoder: banx.NewDefaultGeocoder(hc),
		Communes: communes.MustDefault(),
		DataDir:  "", // embedded-only
	}
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	listingType := reflect.TypeOf(gazetteer.Listing{})
	errType := reflect.TypeOf((*error)(nil)).Elem()
	for _, e := range Entries() {
		src, err := e.Build(deps)
		if err != nil {
			t.Errorf("Build(%q): %v", e.Name, err)
			continue
		}
		m := reflect.ValueOf(src).MethodByName("QueryResult")
		if !m.IsValid() {
			t.Errorf("%s: built Source has no QueryResult method", e.Name)
			continue
		}
		mt := m.Type()
		if mt.NumIn() != 2 || mt.In(0) != ctxType || mt.In(1) != listingType {
			t.Errorf("%s: QueryResult inputs = %v, want (context.Context, gazetteer.Listing)", e.Name, mt)
			continue
		}
		if mt.NumOut() != 2 || mt.Out(1) != errType {
			t.Errorf("%s: QueryResult outputs = %v, want (*Result, error)", e.Name, mt)
			continue
		}
		if mt.Out(0).Kind() != reflect.Pointer {
			t.Errorf("%s: QueryResult first return = %v, want a pointer to the package's Result", e.Name, mt.Out(0))
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

// TestRosterLiveSet pins which sources count as live (may do network
// I/O in Query) — widening or narrowing it changes what
// factory.OfflineSourceNames callers collect, so make it an explicit
// test edit.
func TestRosterLiveSet(t *testing.T) {
	want := map[string]bool{
		"dvf": true, "ademe": true, "bdnb": true, "cadastre": true,
		"georisques": true, "locservice": true, "dpedist": true,
		"education": true, "osm_transit": true,
	}
	for _, e := range Entries() {
		if e.Live != want[e.Name] {
			t.Errorf("%s: Live = %v, want %v", e.Name, e.Live, want[e.Name])
		}
	}
}

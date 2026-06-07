package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestEverySourceDossierJSONRoundtrips is a registry-wide invariant: every
// registered Source's zero-value typed Result must survive a Dossier
// marshal→unmarshal unchanged. The framework reconstitutes Result.Data via the
// factory registered in each source's init(); a Result that does not round-trip
// silently loses its payload in any JSON-persisted Dossier (cache, API, CLI
// --json). One test guards all sources — current and future — mirroring
// TestEverySourceImplementsEmptyReporter.
func TestEverySourceDossierJSONRoundtrips(t *testing.T) {
	for _, name := range gazetteer.RegisteredNames() {
		t.Run(name, func(t *testing.T) {
			factory := gazetteer.Lookup(name)
			if factory == nil {
				t.Fatalf("registered source %q has no result factory", name)
			}
			zero := factory()

			// The typed payload's wire form before any roundtrip.
			wantData, err := json.Marshal(zero)
			if err != nil {
				t.Fatalf("marshal zero Result: %v", err)
			}

			d := gazetteer.Dossier{
				Results: map[string]gazetteer.Result{
					name: {Name: name, Status: gazetteer.StatusOK, Data: zero},
				},
			}
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatalf("marshal Dossier: %v", err)
			}
			var got gazetteer.Dossier
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal Dossier: %v", err)
			}

			r, ok := got.Results[name]
			if !ok || r.Data == nil {
				t.Fatalf("Result.Data dropped on roundtrip (nil after unmarshal)")
			}
			if gotT, wantT := reflect.TypeOf(r.Data), reflect.TypeOf(zero); gotT != wantT {
				t.Fatalf("Result.Data type changed on roundtrip: got %v, want %v", gotT, wantT)
			}
			gotData, err := json.Marshal(r.Data)
			if err != nil {
				t.Fatalf("marshal roundtripped Result: %v", err)
			}
			if string(gotData) != string(wantData) {
				t.Errorf("payload changed on roundtrip:\n before: %s\n  after: %s", wantData, gotData)
			}
		})
	}
}

// TestRenderersNilAndZeroSafe guards the per-source CLI renderers: each must
// tolerate a nil / wrong-type payload AND a zero-value typed Result without
// panicking. A renderer that forgets the `r, ok := data.(*X); if !ok || r == nil`
// guard is caught here instead of crashing `gazetteer query` on an empty source.
func TestRenderersNilAndZeroSafe(t *testing.T) {
	for name, rdr := range sourceRenderers {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("renderer panicked: %v", p)
				}
			}()
			_, _ = rdr(nil) // wrong / absent payload
			if f := gazetteer.Lookup(name); f != nil {
				_, _ = rdr(f()) // zero-value typed Result
			}
		})
	}
}

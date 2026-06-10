package factory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/factory"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/links"
)

// TestNewDefault_Smoke verifies the factory returns a non-nil Client
// wired with every stable in-tree Source. Network failures don't
// fail the test — we only check the wiring shape via a Collect on
// an empty Listing, which produces a Dossier whose Results map
// contains one entry per registered Source.
func TestNewDefault_Smoke(t *testing.T) {
	t.Parallel()
	client, err := factory.NewDefault(context.Background())
	if err != nil {
		t.Fatalf("NewDefault: %v", err)
	}
	if client == nil {
		t.Fatal("NewDefault returned nil Client")
	}
	// Collect with no inputs — every source should either return
	// ErrInsufficientInputs or ErrUnsupportedPropertyType, surfaced
	// as SkippedPrereq Status. Either way the Results map MUST
	// contain one entry per Source.
	d := client.Collect(context.Background(), gazetteer.Listing{})
	wantSources := []string{
		"dvf", "ademe", "anct", "bdnb", "bpe", "georisques", "locservice",
		"carteloyers", "cartofriches", "chomage", "delinquance", "dpedist",
		"education", "encadrement", "filosofi", "qpv", "taxefonciere",
		"vacance", "zonageabc", "zonetendue",
	}
	for _, name := range wantSources {
		if _, ok := d.Results[name]; !ok {
			t.Errorf("Results[%q] missing — factory did not wire it", name)
		}
	}
}

// TestNewDefault_Exclude verifies Options.Exclude prunes the named Sources
// from the default roster (a deny-list) while leaving the rest wired. This is
// the contract consumers rely on to drop Sources they never consume — e.g.
// locador excludes bdnb so its zone report doesn't pay the live BDNB API.
func TestNewDefault_Exclude(t *testing.T) {
	t.Parallel()
	client, err := factory.NewDefaultWith(context.Background(), factory.Options{Exclude: []string{"bdnb", "nonexistent"}})
	if err != nil {
		t.Fatalf("NewDefaultWith: %v", err)
	}
	d := client.Collect(context.Background(), gazetteer.Listing{})
	if _, ok := d.Results["bdnb"]; ok {
		t.Error("Results[\"bdnb\"] present — Exclude did not drop it from the roster")
	}
	// An unrelated default Source must still be wired (Exclude is a deny-list,
	// not an allow-list; unknown names are ignored).
	if _, ok := d.Results["dvf"]; !ok {
		t.Error("Results[\"dvf\"] missing — Exclude over-pruned the roster")
	}
}

// TestNewDefault_SkipNormalizer produces a Client whose Normalize
// returns gazetteer.ErrNormalizerNotConfigured.
func TestNewDefault_SkipNormalizer(t *testing.T) {
	client, err := factory.NewDefaultWith(context.Background(), factory.Options{SkipNormalizer: true})
	if err != nil {
		t.Fatalf("NewDefaultWith: %v", err)
	}
	if _, err := client.Normalize(context.Background(), "1 rue test 75001 Paris"); !errors.Is(err, gazetteer.ErrNormalizerNotConfigured) {
		t.Errorf("SkipNormalizer=true should leave Client.Normalize unconfigured; got %v", err)
	}
}

func TestSourceOverrides(t *testing.T) {
	t.Parallel()

	// Override one roster source; the rest of the roster stays default.
	var got factory.Deps
	b, err := factory.BuilderDefault(context.Background(), factory.Options{
		SourceOverrides: map[string]func(factory.Deps) (gazetteer.Source, error){
			"links": func(d factory.Deps) (gazetteer.Source, error) {
				got = d
				return links.NewSource(links.Options{}), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("BuilderDefault: %v", err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.HTTP == nil || got.Geocoder == nil || got.Communes == nil {
		t.Errorf("override received incomplete Deps: %+v", got)
	}

	// A typo'd override name must fail loudly, not silently keep the default.
	_, err = factory.BuilderDefault(context.Background(), factory.Options{
		SourceOverrides: map[string]func(factory.Deps) (gazetteer.Source, error){
			"linsk": func(factory.Deps) (gazetteer.Source, error) { return nil, nil },
		},
	})
	if err == nil {
		t.Error("unknown override name must error")
	}
}

func TestLiveOfflineSourceNames(t *testing.T) {
	t.Parallel()
	live := factory.LiveSourceNames()
	offline := factory.OfflineSourceNames()
	if len(live) == 0 || len(offline) == 0 {
		t.Fatalf("empty partition: live=%v offline=%v", live, offline)
	}
	seen := map[string]bool{}
	for _, n := range append(append([]string{}, live...), offline...) {
		if seen[n] {
			t.Errorf("source %q in both partitions", n)
		}
		seen[n] = true
	}
	for _, probe := range []string{"dvf", "georisques"} {
		found := false
		for _, n := range live {
			if n == probe {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing from LiveSourceNames", probe)
		}
	}
	for _, n := range offline {
		if n == "dvf" {
			t.Error("dvf must not be offline")
		}
	}
}

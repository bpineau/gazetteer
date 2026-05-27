package factory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/factory"
	"github.com/bpineau/gazetteer/gazetteer"
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
		"dvf", "ademe", "anct", "bdnb", "georisques", "locservice",
		"carteloyers", "cartofriches", "chomage", "delinquance",
		"education", "encadrement", "filosofi", "qpv", "taxefonciere",
		"vacance", "zonageabc", "zonetendue",
	}
	for _, name := range wantSources {
		if _, ok := d.Results[name]; !ok {
			t.Errorf("Results[%q] missing — factory did not wire it", name)
		}
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

package maaddr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/banx/maaddr"
)

// TestStripTrailingZipCity pins the BAN-label trimming helper so a
// retry's autocomplete query is just the street part (the autocomplete-
// shaped caller gets city/zip via its own URL components, not the q=
// parameter).
func TestStripTrailingZipCity(t *testing.T) {
	cases := []struct {
		name  string
		label string
		zip   string
		want  string
	}{
		{
			name:  "standard_paris_label",
			label: "33 boulevard du Château 92210 Saint-Cloud",
			zip:   "92210",
			want:  "33 boulevard du Château",
		},
		{
			name:  "comma_separator",
			label: "8 rue Servan, 75011 Paris",
			zip:   "75011",
			want:  "8 rue Servan",
		},
		{
			name:  "zip_missing_from_label",
			label: "8 rue Servan Paris",
			zip:   "75011",
			want:  "8 rue Servan Paris",
		},
		{
			name:  "empty_zip_passthrough",
			label: "8 rue Servan Paris",
			zip:   "",
			want:  "8 rue Servan Paris",
		},
		{
			name:  "empty_label",
			label: "",
			zip:   "75011",
			want:  "",
		},
		{
			name:  "label_plus_postcode_strips_clean",
			label: "3 boulevard Voltaire 75011 Paris",
			zip:   "75011",
			want:  "3 boulevard Voltaire",
		},
		{
			name:  "label_alone_postcode_empty_passthrough",
			label: "3 boulevard Voltaire 75011 Paris",
			zip:   "",
			want:  "3 boulevard Voltaire 75011 Paris",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := maaddr.StripTrailingZipCity(c.label, c.zip)
			if got != c.want {
				t.Errorf("StripTrailingZipCity(%q, %q) = %q want %q",
					c.label, c.zip, got, c.want)
			}
		})
	}
}

// stubGeocoder is a banx.Geocoder fake used to exercise
// CanonicalizeAddress branches without hitting BAN.
type stubGeocoder struct {
	res banx.GeocodeResult
	err error
}

func (s stubGeocoder) Geocode(_ context.Context, _ banx.GeocodeQuery) (banx.GeocodeResult, error) {
	return s.res, s.err
}

// TestCanonicalizeAddress pins the BAN-normalization branches : nil
// geocoder, BAN error, empty label, no-op normalization, and successful
// strip.
func TestCanonicalizeAddress(t *testing.T) {
	ctx := context.Background()

	t.Run("nil_geocoder_returns_false", func(t *testing.T) {
		got, ok := maaddr.CanonicalizeAddress(ctx, nil,
			"33 boulevard du Château", "Saint-Cloud", "92210")
		if got != "" || ok {
			t.Errorf("nil geocoder = (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("geocoder_error_returns_false", func(t *testing.T) {
		g := stubGeocoder{err: errors.New("ban: 500")}
		got, ok := maaddr.CanonicalizeAddress(ctx, g,
			"33 boulevard du Château", "Saint-Cloud", "92210")
		if got != "" || ok {
			t.Errorf("error = (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("empty_label_returns_false", func(t *testing.T) {
		g := stubGeocoder{res: banx.GeocodeResult{Label: ""}}
		got, ok := maaddr.CanonicalizeAddress(ctx, g,
			"33 boulevard du Château", "Saint-Cloud", "92210")
		if got != "" || ok {
			t.Errorf("empty label = (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("noop_normalization_returns_false", func(t *testing.T) {
		// Stripped label equals the raw address ⇒ caller would burn a
		// 2nd autocomplete call for the same outcome.
		g := stubGeocoder{res: banx.GeocodeResult{
			Label:    "33 boulevard du Château 92210 Saint-Cloud",
			PostCode: "92210",
		}}
		got, ok := maaddr.CanonicalizeAddress(ctx, g,
			"33 boulevard du Château", "Saint-Cloud", "92210")
		if got != "" || ok {
			t.Errorf("noop normalization = (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("successful_normalization", func(t *testing.T) {
		// BAN normalizes the abbreviation "Bd" → "boulevard".
		g := stubGeocoder{res: banx.GeocodeResult{
			Label:    "33 boulevard du Château 92210 Saint-Cloud",
			PostCode: "92210",
		}}
		got, ok := maaddr.CanonicalizeAddress(ctx, g,
			"33 Bd du Château", "Saint-Cloud", "92210")
		if !ok || got != "33 boulevard du Château" {
			t.Errorf("normalize = (%q, %v), want (%q, true)",
				got, ok, "33 boulevard du Château")
		}
	})

	t.Run("postcode_missing_passthrough_returns_label_with_suffix", func(t *testing.T) {
		// When BAN omits PostCode, StripTrailingZipCity is a no-op ;
		// the result still includes the zip+city suffix. The helper
		// still returns true because the result differs from the raw
		// address.
		g := stubGeocoder{res: banx.GeocodeResult{
			Label:    "3 boulevard Voltaire 75011 Paris",
			PostCode: "",
		}}
		got, ok := maaddr.CanonicalizeAddress(ctx, g,
			"3 Bd Voltaire", "Paris", "75011")
		want := "3 boulevard Voltaire 75011 Paris"
		if !ok || got != want {
			t.Errorf("missing-postcode = (%q, %v), want (%q, true)", got, ok, want)
		}
	})
}

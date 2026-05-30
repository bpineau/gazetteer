package zonetendue

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/zonage_tlv_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Non-tendue communes (01999) are dropped; only tendue/touristique kept.
	want := map[string]Entry{
		"01030": {TLV2013: true, Tier: TierTendue},
		"01045": {TLV2013: false, Tier: TierTendueTouristique},
		"75056": {TLV2013: true, Tier: TierTendue},
	}
	if idx.CountTendue() != len(want) {
		t.Fatalf("kept = %d, want %d (non-tendue must be dropped)", idx.CountTendue(), len(want))
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok || got != w {
			t.Errorf("%s: got (%+v,%v), want %+v", insee, got, ok, w)
		}
	}
	if _, ok := idx.Lookup("01999"); ok {
		t.Error("01999 (non-tendue) must be absent from the compact file")
	}

	if idx.Meta.RowCountCommunes != 4 {
		t.Errorf("RowCountCommunes = %d, want 4 (total scanned)", idx.Meta.RowCountCommunes)
	}
	if idx.Meta.RowCountKept != len(want) {
		t.Errorf("RowCountKept = %d, want %d", idx.Meta.RowCountKept, len(want))
	}
	if idx.Meta.EffectiveDate != "2025-12-22" {
		t.Errorf("EffectiveDate = %q, want 2025-12-22", idx.Meta.EffectiveDate)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}

func TestParseSlashDate(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Zonage TLV post décret 22/12/2025": "2025-12-22",
		"décret 1/8/2014":                   "2014-08-01",
		"no date":                           "",
	}
	for in, want := range cases {
		if got := parseSlashDate(in); got != want {
			t.Errorf("parseSlashDate(%q) = %q, want %q", in, got, want)
		}
	}
}

package oll

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded snapshot.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.CommuneCount(); got < 400 {
		t.Errorf("CommuneCount = %d, want ≥ 400", got)
	}
}

// TestQuery_Banlieue resolves a Saint-Denis 2-room flat to its observed rent.
func TestQuery_Banlieue(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{
		INSEE: "93066", PropertyType: gazetteer.PropertyApartment, Rooms: new(2),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatalf("empty result for Saint-Denis T2")
	}
	if res.Evidence.AggloCode != "L7502" || res.Evidence.ZoneID != "5" {
		t.Errorf("resolved to agglo %q zone %q, want L7502/5", res.Evidence.AggloCode, res.Evidence.ZoneID)
	}
	if res.Pieces != 2 {
		t.Errorf("Pieces = %d, want 2", res.Pieces)
	}
	if res.ObservedMedianEURPerM2 < 10 || res.ObservedMedianEURPerM2 > 30 {
		t.Errorf("median = %.1f, want a sane €/m² in [10,30]", res.ObservedMedianEURPerM2)
	}
	if res.ObservedQ1EURPerM2 > res.ObservedMedianEURPerM2 || res.ObservedMedianEURPerM2 > res.ObservedQ3EURPerM2 {
		t.Errorf("quartile order broken: q1=%.1f med=%.1f q3=%.1f", res.ObservedQ1EURPerM2, res.ObservedMedianEURPerM2, res.ObservedQ3EURPerM2)
	}
	if res.SampleSize <= 0 || res.Confidence == ConfidenceNone {
		t.Errorf("sample/confidence = %d/%q, want populated", res.SampleSize, res.Confidence)
	}
	// RentEstimate feeds the appraisal layer.
	est := res.RentEstimate()
	if est.EurPerM2Cents <= 0 {
		t.Errorf("RentEstimate cents = %d, want > 0", est.EurPerM2Cents)
	}
}

// TestQuery_NoRooms falls back to the zone-level all-sizes aggregate when the
// listing carries no room count, so OLL still contributes a reading.
func TestQuery_NoRooms(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{
		INSEE: "93066", PropertyType: gazetteer.PropertyApartment,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatalf("empty result for Saint-Denis without rooms; want zone aggregate")
	}
	if res.Pieces != 0 {
		t.Errorf("Pieces = %d, want 0 (all-sizes aggregate)", res.Pieces)
	}
	if res.Evidence.ZoneID != "5" || res.ObservedMedianEURPerM2 <= 0 {
		t.Errorf("aggregate resolve failed: zone %q median %.1f", res.Evidence.ZoneID, res.ObservedMedianEURPerM2)
	}
}

// TestQuery_RoomsGradient checks the observed €/m² falls as rooms grow (a known
// property of the rent surface) within a single zone.
func TestQuery_RoomsGradient(t *testing.T) {
	t.Parallel()
	var prev float64
	for rooms := 1; rooms <= 4; rooms++ {
		res, err := Query(context.Background(), Options{}, gazetteer.Listing{
			INSEE: "93066", PropertyType: gazetteer.PropertyApartment, Rooms: new(rooms),
		})
		if err != nil {
			t.Fatalf("Query rooms=%d: %v", rooms, err)
		}
		if res.IsEmpty() {
			continue
		}
		if prev != 0 && res.ObservedMedianEURPerM2 > prev+0.5 {
			t.Errorf("rooms=%d median %.1f > rooms=%d %.1f (€/m² should not rise with size)", rooms, res.ObservedMedianEURPerM2, rooms-1, prev)
		}
		prev = res.ObservedMedianEURPerM2
	}
}

// TestQuery_OutOfPerimeter returns empty outside the covered agglomeration.
func TestQuery_OutOfPerimeter(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{
		INSEE: "69383", PropertyType: gazetteer.PropertyApartment, Rooms: new(2), // Lyon 3e
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty() = false, want true for Lyon (outside L7502)")
	}
}

// TestQuery_Skips covers the prerequisite skips.
func TestQuery_Skips(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		l    gazetteer.Listing
		want error
	}{
		{"no insee", gazetteer.Listing{PropertyType: gazetteer.PropertyApartment, Rooms: new(2)}, gazetteer.ErrInsufficientInputs},
		{"land", gazetteer.Listing{INSEE: "93066", PropertyType: gazetteer.PropertyLand, Rooms: new(2)}, gazetteer.ErrUnsupportedPropertyType},
		{"commercial", gazetteer.Listing{INSEE: "93066", PropertyType: gazetteer.PropertyCommercial, Rooms: new(2)}, gazetteer.ErrUnsupportedPropertyType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Query(context.Background(), Options{}, c.l); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

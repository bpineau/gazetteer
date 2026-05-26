package gazetteer

import (
	"encoding/json"
	"testing"
	"time"
)

func TestListing_ZeroValue(t *testing.T) {
	var l Listing
	if l.Address != "" {
		t.Errorf("Address = %q, want empty", l.Address)
	}
	if l.Lat != nil {
		t.Errorf("Lat = %v, want nil", l.Lat)
	}
	if l.PropertyType != PropertyUnknown {
		t.Errorf("PropertyType = %v, want PropertyUnknown", l.PropertyType)
	}
}

func TestListing_JSONRoundtrip(t *testing.T) {
	lat, lon := 48.8566, 2.3522
	surface := 42.0
	rooms := 2
	year := 1900
	orig := Listing{
		Address:      "10 rue de la Paix",
		City:         "Paris",
		Zip:          "75002",
		INSEE:        "75102",
		Lat:          &lat,
		Lon:          &lon,
		PropertyType: PropertyApartment,
		SurfaceM2:    &surface,
		Rooms:        &rooms,
		BuildYear:    &year,
		AsOf:         time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Listing
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Address != orig.Address || got.PropertyType != orig.PropertyType {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, orig)
	}
	if got.Lat == nil || *got.Lat != lat {
		t.Errorf("Lat lost: %v", got.Lat)
	}
}

func TestPropertyType_Values(t *testing.T) {
	cases := []struct {
		v    PropertyType
		want string
	}{
		{PropertyUnknown, ""},
		{PropertyApartment, "apartment"},
		{PropertyHouse, "house"},
		{PropertyLand, "land"},
		{PropertyCommercial, "commercial"},
	}
	for _, c := range cases {
		if string(c.v) != c.want {
			t.Errorf("PropertyType(%v) = %q, want %q", c.v, string(c.v), c.want)
		}
	}
}

func TestListing_EmptyMarshalsToEmptyObject(t *testing.T) {
	b, err := json.Marshal(Listing{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != "{}" {
		t.Errorf("zero Listing marshals to %s, want {}", b)
	}
}

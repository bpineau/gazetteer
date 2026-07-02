package osm

import (
	"reflect"
	"testing"
)

// routesFixture is a minimal but structurally-faithful Overpass routes
// response: one métro route with direct stop members (the Paris
// pattern), one tram route, one stop_area linking a station node to a
// stop_position (the Lyon pattern), plus rejects (unknown mode, no
// ref, stop_area without nodes).
const routesFixture = `{
  "elements": [
    {
      "type": "relation", "id": 100,
      "tags": {"type": "route", "route": "subway", "ref": "8", "network": "RATP"},
      "members": [
        {"type": "node", "ref": 1, "role": "stop"},
        {"type": "node", "ref": 2, "role": "platform"},
        {"type": "way",  "ref": 3, "role": ""}
      ]
    },
    {
      "type": "relation", "id": 101,
      "tags": {"type": "route", "route": "tram", "short_name": "T3a"},
      "members": [{"type": "node", "ref": 4, "role": "stop"}]
    },
    {
      "type": "relation", "id": 102,
      "tags": {"type": "route", "route": "bus", "ref": "96"},
      "members": [{"type": "node", "ref": 5, "role": "stop"}]
    },
    {
      "type": "relation", "id": 103,
      "tags": {"type": "route", "route": "train"},
      "members": [{"type": "node", "ref": 6, "role": "stop"}]
    },
    {
      "type": "relation", "id": 200,
      "tags": {"public_transport": "stop_area"},
      "members": [
        {"type": "node", "ref": 1, "role": "stop"},
        {"type": "node", "ref": 10, "role": ""}
      ]
    },
    {
      "type": "relation", "id": 201,
      "tags": {"public_transport": "stop_area"},
      "members": [{"type": "way", "ref": 7, "role": ""}]
    }
  ]
}`

func TestParseOverpassRoutes(t *testing.T) {
	routes, areas, err := ParseOverpassRoutes([]byte(routesFixture))
	if err != nil {
		t.Fatalf("ParseOverpassRoutes: %v", err)
	}

	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2 (bus and ref-less train dropped): %+v", len(routes), routes)
	}
	metro := routes[0]
	if metro.Ref != "8" || metro.Mode != "subway" || metro.Network != "RATP" {
		t.Errorf("métro route mis-parsed: %+v", metro)
	}
	if !reflect.DeepEqual(metro.Stops, []int64{1, 2}) {
		t.Errorf("métro stops = %v, want [1 2] (way member dropped)", metro.Stops)
	}
	if tram := routes[1]; tram.Ref != "3a" {
		// short_name "T3a" minus the mode prefix.
		t.Errorf("tram ref = %q, want %q (short_name fallback)", tram.Ref, "3a")
	}

	if len(areas) != 1 {
		t.Fatalf("stop_areas = %d, want 1 (node-less area dropped): %+v", len(areas), areas)
	}
	if !reflect.DeepEqual(areas[0].Nodes, []int64{1, 10}) {
		t.Errorf("stop_area nodes = %v", areas[0].Nodes)
	}
}

func TestParseOverpassRoutes_Errors(t *testing.T) {
	if _, _, err := ParseOverpassRoutes(nil); err == nil {
		t.Error("empty body should error")
	}
	if _, _, err := ParseOverpassRoutes([]byte("{broken")); err == nil {
		t.Error("malformed JSON should error")
	}
}

func TestModesForType(t *testing.T) {
	cases := map[TransitType][]string{
		TransitTypeMetro:      {"subway", "light_rail"},
		TransitTypeTram:       {"tram"},
		TransitTypeRER:        {"train"},
		TransitTypeTransilien: {"train"},
		TransitTypeTrain:      {"train"},
	}
	for tt, wantModes := range cases {
		got := modesForType(tt)
		if len(got) != len(wantModes) {
			t.Errorf("modesForType(%s) = %v, want %v", tt, got, wantModes)
			continue
		}
		for _, m := range wantModes {
			if _, ok := got[m]; !ok {
				t.Errorf("modesForType(%s) missing %q", tt, m)
			}
		}
	}
	if got := modesForType(TransitType("boat")); got != nil {
		t.Errorf("unknown type should yield nil, got %v", got)
	}
}

func TestAttachLinesFromRoutes(t *testing.T) {
	routes := []Route{
		{ID: 100, Mode: "subway", Ref: "8", Stops: []int64{1}},
		{ID: 101, Mode: "subway", Ref: "9", Stops: []int64{1}},
		{ID: 102, Mode: "train", Ref: "P", Stops: []int64{1}}, // wrong mode for a métro station
		{ID: 103, Mode: "tram", Ref: "T1", Stops: []int64{40}},
	}
	areas := []StopArea{
		// Lyon pattern: station node 20 shares a stop_area with
		// stop_position 40, which is the actual route member.
		{ID: 200, Nodes: []int64{20, 40}},
	}
	stations := []Station{
		{OSMID: 1, Type: TransitTypeMetro},                         // direct membership
		{OSMID: 20, Type: TransitTypeTram},                         // via stop_area
		{OSMID: 30, Type: TransitTypeMetro, Lines: []string{"14"}}, // pre-tagged: untouched
		{OSMID: 50, Type: TransitTypeMetro},                        // no membership at all
	}

	AttachLinesFromRoutes(stations, routes, areas)

	if want := []string{"8", "9"}; !reflect.DeepEqual(stations[0].Lines, want) {
		t.Errorf("direct station lines = %v, want %v (sorted, train ref filtered out)", stations[0].Lines, want)
	}
	if want := []string{"T1"}; !reflect.DeepEqual(stations[1].Lines, want) {
		t.Errorf("stop_area station lines = %v, want %v", stations[1].Lines, want)
	}
	if want := []string{"14"}; !reflect.DeepEqual(stations[2].Lines, want) {
		t.Errorf("pre-tagged station must keep its explicit lines, got %v", stations[2].Lines)
	}
	if stations[3].Lines != nil {
		t.Errorf("unmatched station should stay line-less, got %v", stations[3].Lines)
	}
}

func TestAttachLinesFromRoutes_NoInputs(t *testing.T) {
	stations := []Station{{OSMID: 1, Type: TransitTypeMetro}}
	AttachLinesFromRoutes(stations, nil, nil) // must not panic nor mutate
	if stations[0].Lines != nil {
		t.Errorf("no routes: lines should stay nil, got %v", stations[0].Lines)
	}
}

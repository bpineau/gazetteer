package osm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClassifyType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tags   map[string]string
		want   TransitType
		accept bool
	}{
		{"tram stop", map[string]string{"railway": "tram_stop"}, TransitTypeTram, true},
		{"tram flag", map[string]string{"railway": "station", "tram": "yes"}, TransitTypeTram, true},
		// A tram flag wins over the subway one: railway=tram_stop is the
		// most specific tag French data carries.
		{"tram stop that also flags subway", map[string]string{"railway": "tram_stop", "subway": "yes"}, TransitTypeTram, true},
		{"station=subway", map[string]string{"railway": "station", "station": "subway"}, TransitTypeMetro, true},
		{"station=light_rail", map[string]string{"railway": "station", "station": "light_rail"}, TransitTypeMetro, true},
		{"subway flag", map[string]string{"railway": "station", "subway": "yes"}, TransitTypeMetro, true},
		{"light_rail flag", map[string]string{"railway": "station", "light_rail": "yes"}, TransitTypeMetro, true},
		// Subway precedence at an interchange (Châtelet carries all three).
		{"subway wins over train", map[string]string{"subway": "yes", "train": "yes", "network": "RER"}, TransitTypeMetro, true},
		{"RER by network", map[string]string{"railway": "station", "network": "RER"}, TransitTypeRER, true},
		// The RER detector matches the ACRONYM (case-insensitively), so the
		// spelled-out French network name is not recognised and the station
		// lands in the generic train bucket. Pinned as-is: changing it would
		// re-label part of the shipped catalog.
		{"spelled-out RER network", map[string]string{"railway": "station", "network": "Réseau express régional"}, TransitTypeTrain, true},
		{"RER by route_ref", map[string]string{"railway": "station", "route_ref": "RER B"}, TransitTypeRER, true},
		{"RER wins over train", map[string]string{"train": "yes", "network": "rer"}, TransitTypeRER, true},
		{"transilien", map[string]string{"train": "yes", "network": "Transilien"}, TransitTypeTransilien, true},
		{"mainline train", map[string]string{"train": "yes", "network": "TER Grand Est"}, TransitTypeTrain, true},
		{"bare station", map[string]string{"railway": "station"}, TransitTypeTrain, true},
		{"bare halt", map[string]string{"railway": "halt"}, TransitTypeTrain, true},
		{"halt on the transilien network", map[string]string{"railway": "halt", "network": "Transilien Ligne J"}, TransitTypeTransilien, true},
		// A funicular is kept out by the QL, not here: should one reach the
		// parser anyway, railway=station makes it a generic train.
		{"funicular tagged as a railway station", map[string]string{"railway": "station", "station": "funicular"}, TransitTypeTrain, true},
		// Nothing in our target modes: the element is dropped upstream.
		{"aerialway station", map[string]string{"aerialway": "station"}, "", false},
		{"platform only", map[string]string{"public_transport": "platform"}, "", false},
		{"no usable tag", map[string]string{"name": "nowhere"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, accept := classifyType(c.tags)
			if got != c.want || accept != c.accept {
				t.Errorf("classifyType = (%q, %v), want (%q, %v)", got, accept, c.want, c.accept)
			}
		})
	}
}

func TestIsBusOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tags map[string]string
		want bool
	}{
		{"railway bus stop", map[string]string{"railway": "bus_stop"}, true},
		{"highway bus stop", map[string]string{"highway": "bus_stop"}, true},
		{"platform, bus only", map[string]string{"public_transport": "platform", "bus": "yes"}, true},
		{"stop position, bus only", map[string]string{"public_transport": "stop_position", "bus": "yes"}, true},
		{"bus and subway", map[string]string{"bus": "yes", "subway": "yes"}, false},
		{"bus and light_rail", map[string]string{"bus": "yes", "light_rail": "yes"}, false},
		{"bus and train", map[string]string{"bus": "yes", "train": "yes"}, false},
		{"bus and tram", map[string]string{"bus": "yes", "tram": "yes"}, false},
		{"bus at a railway station", map[string]string{"bus": "yes", "railway": "station"}, false},
		{"bus at a halt", map[string]string{"bus": "yes", "railway": "halt"}, false},
		{"bus at a tram stop", map[string]string{"bus": "yes", "railway": "tram_stop"}, false},
		// A bus flag on something that is neither rail nor a known station
		// tag stays excluded: defence in depth behind the QL filter.
		{"bus at a level crossing", map[string]string{"bus": "yes", "railway": "level_crossing"}, true},
		{"no bus at all", map[string]string{"railway": "station"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isBusOnly(c.tags); got != c.want {
				t.Errorf("isBusOnly(%v) = %v, want %v", c.tags, got, c.want)
			}
		})
	}
}

func TestIsGhostStation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tags map[string]string
		want bool
	}{
		{"disused flag", map[string]string{"disused": "yes"}, true},
		{"abandoned flag", map[string]string{"abandoned": "yes"}, true},
		{"station:disused", map[string]string{"station:disused": "yes"}, true},
		{"disused railway prefix", map[string]string{"disused:railway": "station"}, true},
		{"disused public_transport prefix", map[string]string{"disused:public_transport": "station"}, true},
		{"abandoned railway prefix", map[string]string{"abandoned:railway": "halt"}, true},
		{"abandoned public_transport prefix", map[string]string{"abandoned:public_transport": "station"}, true},
		{"razed prefix", map[string]string{"razed:railway": "station"}, true},
		{"demolished prefix", map[string]string{"demolished:building": "yes"}, true},
		{"live station", map[string]string{"railway": "station", "name": "Lourmel"}, false},
		{"disused=no", map[string]string{"disused": "no", "railway": "station"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isGhostStation(c.tags); got != c.want {
				t.Errorf("isGhostStation(%v) = %v, want %v", c.tags, got, c.want)
			}
		})
	}
}

func TestCoordinates(t *testing.T) {
	t.Parallel()
	lat, lon := 48.84, 2.29
	cases := []struct {
		name    string
		el      overpassElement
		wantLat float64
		wantOK  bool
	}{
		{"node carries its own coords", overpassElement{Lat: &lat, Lon: &lon}, lat, true},
		{
			name:    "way carries a synthesised centre",
			el:      overpassElement{Center: &overpassCenter{Lat: 45.76, Lon: 4.86}},
			wantLat: 45.76,
			wantOK:  true,
		},
		{
			// A node with only one of the two is unusable: the pair is what
			// makes a coordinate.
			name:   "half a coordinate",
			el:     overpassElement{Lat: &lat},
			wantOK: false,
		},
		{"nothing to locate", overpassElement{}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gotLat, _, ok := coordinates(c.el)
			if ok != c.wantOK || gotLat != c.wantLat {
				t.Errorf("coordinates = (%v, %v), want (%v, %v)", gotLat, ok, c.wantLat, c.wantOK)
			}
		})
	}
}

// TestParseOverpass_ExclusionsAndOrder walks one synthetic payload carrying
// every drop rule at once, then pins the deterministic output order the
// on-disk catalog depends on.
func TestParseOverpass_ExclusionsAndOrder(t *testing.T) {
	t.Parallel()
	body := []byte(`{"elements":[
      {"type":"node","id":30,"lat":48.85,"lon":2.35,"tags":{"name":"Zoo","railway":"station","subway":"yes"}},
      {"type":"node","id":10,"lat":48.85,"lon":2.35,"tags":{"name":"Alpha","railway":"station","subway":"yes"}},
      {"type":"node","id":9,"lat":48.85,"lon":2.35,"tags":{"name":"Alpha","railway":"halt"}},
      {"type":"way","id":40,"center":{"lat":48.86,"lon":2.36},"tags":{"name":"Ensemble gare","railway":"station"}},
      {"type":"node","id":50,"lat":48.85,"lon":2.35},
      {"type":"node","id":51,"lat":48.85,"lon":2.35,"tags":{"railway":"station","subway":"yes"}},
      {"type":"node","id":52,"lat":48.85,"lon":2.35,"tags":{"name":"  ","railway":"station"}},
      {"type":"node","id":53,"lat":48.85,"lon":2.35,"tags":{"name":"Ghost","railway":"station","disused":"yes"}},
      {"type":"node","id":54,"lat":48.85,"lon":2.35,"tags":{"name":"Bus","railway":"bus_stop"}},
      {"type":"node","id":55,"tags":{"name":"Nowhere","railway":"station"}},
      {"type":"node","id":56,"lat":48.85,"lon":2.35,"tags":{"name":"Funi","railway":"station","station":"funicular"}}
    ]}`)
	stations, err := ParseOverpass(body)
	if err != nil {
		t.Fatalf("ParseOverpass: %v", err)
	}
	var got []string
	for _, s := range stations {
		got = append(got, s.Name)
	}
	// "Funi" survives: railway=station without a mode sub-tag is a generic
	// train, funicular sub-tag or not (the QL is what keeps those out).
	want := []string{"Alpha", "Alpha", "Ensemble gare", "Funi", "Zoo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stations = %v, want %v (name then OSMID, everything else dropped)", got, want)
	}
	if stations[0].OSMID != 9 || stations[1].OSMID != 10 {
		t.Errorf("same-name stations must be ordered by OSMID, got %d then %d", stations[0].OSMID, stations[1].OSMID)
	}
	if stations[2].OSMType != "way" || stations[2].Lat != 48.86 {
		t.Errorf("way station mis-parsed: %+v", stations[2])
	}
}

func TestParseOverpass_Errors(t *testing.T) {
	t.Parallel()
	if _, err := ParseOverpass(nil); err == nil {
		t.Error("an empty body must be an error, not an empty catalog")
	}
	if _, err := ParseOverpass([]byte("{not json")); err == nil {
		t.Error("malformed JSON must be an error")
	}
	// A well-formed payload with nothing usable is NOT an error.
	st, err := ParseOverpass([]byte(`{"elements":[]}`))
	if err != nil || len(st) != 0 {
		t.Errorf("empty elements = (%v, %v), want (no stations, nil)", st, err)
	}
}

func TestParseLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tags map[string]string
		want []string
	}{
		{"single ref", map[string]string{"ref": "8"}, []string{"8"}},
		{"semicolon list", map[string]string{"ref": "3a;3b"}, []string{"3a", "3b"}},
		{"route_ref", map[string]string{"route_ref": "A;B"}, []string{"A", "B"}},
		{"line tag", map[string]string{"line": "14"}, []string{"14"}},
		{"deduplicated across tags", map[string]string{"ref": "8", "route_ref": "8;10"}, []string{"8", "10"}},
		{"blank parts dropped", map[string]string{"ref": " 8 ;; ;9"}, []string{"8", "9"}},
		{"nothing", map[string]string{"name": "Lourmel"}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := parseLines(c.tags); !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseLines(%v) = %v, want %v", c.tags, got, c.want)
			}
		})
	}
}

// TestStationDisplay_UnclassifiedType: Display used to slice the first letter
// out of the mode name unguarded, so a Station with lines but no Type (a
// hand-built one, or one decoded from a catalog written elsewhere) panicked
// with an index-out-of-range.
func TestStationDisplay_UnclassifiedType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		st   Station
		want string
	}{
		{"no type, one line", Station{Name: "Lourmel", Lines: []string{"8"}}, "Lourmel (8)"},
		{"no type, two lines", Station{Name: "Lourmel", Lines: []string{"8", "10"}}, "Lourmel (8/10)"},
		{"no type, no line", Station{Name: "Lourmel"}, "Lourmel"},
		{"unknown type", Station{Name: "Funi", Type: TransitType("funicular"), Lines: []string{"F"}}, "Funi (FF)"},
		{"train", Station{Name: "Meaux", Type: TransitTypeTrain, Lines: []string{"P"}}, "Meaux (TP)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.st.Display(); got != c.want {
				t.Errorf("Display = %q, want %q", got, c.want)
			}
		})
	}
}

func TestWalkingConversions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		haversine float64
		wantWalk  int
		wantMin   int
	}{
		{"zero", 0, 0, 1},                   // the minute floor is 1, never 0
		{"negative is clamped", -100, 0, 1}, // a caller passing junk gets 0 m, not a negative walk
		{"100 m", 100, 130, 2},
		{"rounds to the nearest metre", 100.4, 131, 2},
		{"1 km", 1000, 1300, 16},
		{"the proximity cap", MaxNearestStationMeters, 6500, 81},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			walk := WalkingMetersFromHaversine(c.haversine)
			if walk != c.wantWalk {
				t.Errorf("WalkingMetersFromHaversine(%v) = %d, want %d", c.haversine, walk, c.wantWalk)
			}
			if got := WalkMinutes(walk); got != c.wantMin {
				t.Errorf("WalkMinutes(%d) = %d, want %d", walk, got, c.wantMin)
			}
		})
	}
}

func TestResult_IsEmptyNil(t *testing.T) {
	t.Parallel()
	var r *Result
	if !r.IsEmpty() {
		t.Error("(*Result)(nil).IsEmpty() = false, want true")
	}
	if (&Result{SampleSize: 1}).IsEmpty() {
		t.Error("a picked station must not report empty")
	}
}

// TestLoadCatalog_UnreadableFile covers the two hard failures LoadCatalog
// does surface (as opposed to the misses it reports as nil, nil).
func TestLoadCatalog_UnreadableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(bad); err == nil || !strings.Contains(err.Error(), "parse catalog") {
		t.Errorf("LoadCatalog(garbage) = %v, want a parse error", err)
	}
	// A directory where a file is expected is a read error, not a miss.
	if _, err := LoadCatalog(dir); err == nil {
		t.Error("LoadCatalog(directory) = nil error, want a read error")
	}
}

// TestSaveCatalog_MkdirFailure: the parent directory is created on demand,
// so a FILE sitting where that directory should go must be reported.
func TestSaveCatalog_MkdirFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "osm")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SaveCatalog(filepath.Join(blocker, "transit_stations.json"), &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		Stations:      []Station{{OSMID: 1, Name: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("SaveCatalog under a file = %v, want a mkdir error", err)
	}
}

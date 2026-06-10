package osm

import "time"

// FranceMetropolitanBBox is the bounding box (south,west,north,east)
// covering metropolitan France including Corsica. Chosen wide enough to
// catch a station on the Belgian / German / Swiss border that an
// auction near the frontier may legitimately depend on, but tight
// enough that the Overpass response stays well under the 1 GB
// soft-cap. Order matches Overpass QL convention.
const FranceMetropolitanBBox = "41.0,-5.5,51.5,10.0"

// OverpassEndpoint is the default public Overpass interpreter.
// Exposed as `var` so tests can swap in an httptest.NewServer URL.
//
// overpass.openstreetmap.fr is intentionally excluded from the rotation:
// the mirror replies 403 "This service is only available to white-listed
// usages" on every anonymous request, so every probe is a guaranteed
// round-trip waste before the fallback rescues the call.
var OverpassEndpoint = "https://overpass.private.coffee/api/interpreter" //nolint:gosec // public API endpoint URL, not a credential

// OverpassFallbackEndpoints is tried in order when OverpassEndpoint fails.
// Having at least one fallback makes catalog refresh resilient to individual
// mirror outages (overpass-api.de has recurring 406-at-Apache episodes).
var OverpassFallbackEndpoints = []string{ //nolint:gosec // public API endpoint URLs, not credentials
	"https://overpass-api.de/api/interpreter",
}

// OverpassTimeoutSeconds is the per-query upper bound advertised to the
// Overpass interpreter via the `[timeout:N]` setting. The server-side
// limit is 180 s on the public instance — we pin to 180 s to match.
const OverpassTimeoutSeconds = 180

// OverpassMaxSizeMB is the per-query memory budget advertised via the
// `[maxsize:N]` setting for per-department sub-queries. Each department
// is expected to contain at most a few hundred stations so 64 MB per
// query is well above the real-world ceiling.
const OverpassMaxSizeMB = 64

// OverpassDeptTimeout is the per-department context deadline used by
// RefreshCatalogFromOverpassByDepts. It must cover a full mirror walk:
// each mirror gets its own ~20 s slice inside HTTPOverpassFetcher.Query
// (so a hung primary cannot starve the fallback), so the dept budget is
// sized at slices × mirrors + slack. A healthy response takes 2-5 s.
const OverpassDeptTimeout = 45 * time.Second

// MinExpectedStations is the absolute floor below which a France-wide
// refresh is considered failed even when every per-dept HTTP call returned
// 200 OK. Empirically the full metropolitan catalog carries ~9 000 stations;
// 2 000 is a conservative ~22 % of that — well below the variance of OSM
// tag churn but well above any scenario where most departments
// legitimately returned data.
//
// Below this floor the refresh aborts WITHOUT overwriting the on-disk
// catalog, so the operator can investigate (mirror outage, silent empty
// responses, geo bbox regression) instead of consuming a partial garbage
// snapshot at the next enrich run.
//
// Exposed as `var` so tests can lower it for the duration of a single
// case (the synthetic Overpass fixtures only produce a handful of
// stations). Production callers never mutate this value.
var MinExpectedStations = 2000 //nolint:gochecknoglobals // intentional test-override knob

// DeptBBox is one entry in the FranceDepartmentBBoxes table.
// Code is the INSEE department code (01-95, 2A/2B for Corsica).
// BBox is "south,west,north,east" in the Overpass QL convention.
type DeptBBox struct {
	Code string
	BBox string
}

// FranceDepartmentBBoxes contains approximate bounding boxes for all
// 96 metropolitan departments (mainland + Corsica). Values are padded by
// ~0.15° so that stations straddling a border (e.g. a suburban halt
// shared between two departments) are captured. The list is stable and
// matches the INSEE department numbering.
//
// Using per-department bboxes instead of a single France-wide bbox
// avoids Overpass timeouts on public mirrors that refuse queries whose
// estimated cost exceeds their server-side budget.
var FranceDepartmentBBoxes = []DeptBBox{ //nolint:gochecknoglobals // package-level constants table
	{"01", "45.77,4.76,46.53,5.92"},
	{"02", "49.00,3.00,50.10,4.25"},
	{"03", "45.95,2.31,46.86,3.98"},
	{"04", "43.70,5.67,44.70,7.02"},
	{"05", "44.15,5.61,45.20,6.95"},
	{"06", "43.48,6.63,44.36,7.72"},
	{"07", "44.28,3.99,45.55,4.85"},
	{"08", "49.27,4.05,50.17,5.36"},
	{"09", "42.60,0.85,43.38,2.10"},
	{"10", "47.85,3.47,48.75,4.82"},
	{"11", "42.65,1.67,43.55,3.25"},
	{"12", "43.83,1.80,44.98,3.30"},
	{"13", "43.15,4.58,43.98,5.79"},
	{"14", "48.73,-0.80,49.45,0.52"},
	{"15", "44.60,1.90,45.55,3.35"},
	{"16", "45.24,-0.48,46.12,0.90"},
	{"17", "45.13,-1.43,46.30,-0.12"},
	{"18", "46.46,1.78,47.62,3.08"},
	{"19", "44.97,1.33,45.90,2.72"},
	{"2A", "41.35,8.53,41.95,9.40"},
	{"2B", "41.85,8.67,43.03,9.58"},
	{"21", "46.78,4.10,48.02,5.60"},
	{"22", "47.89,-3.77,48.84,-1.90"},
	{"23", "45.62,1.73,46.35,2.95"},
	{"24", "44.35,-0.90,45.73,1.48"},
	{"25", "46.73,5.63,47.75,7.10"},
	{"26", "44.12,4.68,45.38,5.85"},
	{"27", "48.72,0.63,49.65,1.95"},
	{"28", "47.78,0.73,49.00,2.02"},
	{"29", "47.58,-5.15,48.85,-3.40"},
	{"30", "43.52,3.58,44.55,4.90"},
	{"31", "42.68,0.65,43.98,2.33"},
	{"32", "43.35,-0.32,44.20,1.22"},
	{"33", "44.18,-1.35,45.57,-0.07"},
	{"34", "43.25,3.00,43.97,4.20"},
	{"35", "47.63,-2.22,48.55,-1.00"},
	{"36", "46.32,1.08,47.22,2.35"},
	{"37", "46.88,0.08,47.73,1.32"},
	{"38", "44.72,4.77,45.97,6.38"},
	{"39", "46.27,5.28,47.48,6.32"},
	{"40", "43.45,-1.55,44.68,0.03"},
	{"41", "47.23,0.82,48.22,2.22"},
	{"42", "45.24,3.73,46.27,4.78"},
	{"43", "44.80,3.08,45.65,4.40"},
	{"44", "46.83,-2.65,47.82,-1.00"},
	{"45", "47.45,1.57,48.57,3.13"},
	{"46", "44.35,1.22,45.15,2.68"},
	{"47", "43.88,-0.05,44.80,1.32"},
	{"48", "44.13,2.88,45.08,3.97"},
	{"49", "46.87,-2.05,47.98,-0.02"},
	{"50", "48.45,-1.95,49.73,-0.80"},
	{"51", "48.52,3.40,49.55,5.15"},
	{"52", "47.55,4.70,48.72,5.98"},
	{"53", "47.72,-1.20,48.55,-0.08"},
	{"54", "48.55,5.58,49.65,7.05"},
	{"55", "48.42,4.90,49.50,5.97"},
	{"56", "47.32,-3.68,48.20,-2.08"},
	{"57", "48.75,6.05,49.85,7.65"},
	{"58", "46.60,3.00,47.83,4.35"},
	{"59", "50.05,2.00,51.10,3.87"},
	{"60", "49.03,1.73,49.87,3.18"},
	{"61", "48.18,-0.73,48.97,0.97"},
	{"62", "50.00,1.57,51.00,3.02"},
	{"63", "45.20,2.50,46.10,3.98"},
	{"64", "42.97,-1.80,43.78,0.05"},
	{"65", "42.72,-0.33,43.68,0.40"},
	{"66", "42.33,1.73,42.98,3.22"},
	{"67", "47.80,6.83,49.08,8.28"},
	{"68", "47.42,6.85,48.37,7.65"},
	{"69", "45.46,4.47,46.30,5.18"},
	{"70", "47.22,5.55,47.97,6.87"},
	{"71", "46.17,3.87,47.05,5.48"},
	{"72", "47.60,-0.47,48.57,0.88"},
	{"73", "45.12,5.83,45.93,7.12"},
	{"74", "45.60,5.82,46.42,7.05"},
	{"75", "48.81,2.22,48.91,2.42"},
	{"76", "49.23,0.08,50.07,1.97"},
	{"77", "48.08,2.50,49.08,3.62"},
	{"78", "48.58,1.42,49.17,2.22"},
	{"79", "46.15,-0.72,47.18,0.32"},
	{"80", "49.52,1.37,50.37,3.30"},
	{"81", "43.48,1.65,44.22,2.92"},
	{"82", "43.78,0.75,44.43,1.95"},
	{"83", "43.00,5.67,43.97,6.97"},
	{"84", "43.70,4.65,44.37,5.88"},
	{"85", "46.27,-2.47,47.17,-0.58"},
	{"86", "46.22,-0.05,47.13,1.18"},
	{"87", "45.47,0.93,46.38,2.07"},
	{"88", "47.82,5.60,48.73,7.20"},
	{"89", "47.45,2.88,48.40,4.35"},
	{"90", "47.42,6.75,47.82,7.18"},
	{"91", "48.27,1.93,48.75,2.67"},
	{"92", "48.78,2.15,48.96,2.35"},
	{"93", "48.83,2.33,49.00,2.63"},
	{"94", "48.71,2.27,48.88,2.58"},
	{"95", "48.92,1.87,49.23,2.52"},
}

// FranceTransitOverpassQL is the canonical Overpass QL query that
// enumerates every train-based public-transport station in
// metropolitan France. Exposed via a function so the bbox can be
// overridden in tests (testdata fixture covers Paris XV only).
//
// What we collect (cf. SPECS source-discovery §OSM Overpass) :
//
//   - railway=station  with station ∈ {subway, light_rail} or
//     subway=yes / light_rail=yes / train=yes
//     → métro, RER, Transilien, train de banlieue.
//   - railway=halt    → smaller suburban rail halts (Transilien fan-out).
//   - railway=tram_stop → tramway.
//   - public_transport=station with subway=yes / light_rail=yes /
//     train=yes — modern OSM tagging schema (post 2019), used in
//     parallel with `railway=` because not every contributor migrated.
//
// What we EXCLUDE :
//
//   - bus_stop / highway=bus_stop / public_transport=platform with bus=yes
//     (operator MVP : train-class only).
//   - disused:railway=*, abandoned:railway=*, station:disused=yes
//     (ghost stations).
//   - station=funicular / monorail / aerialway (out of scope at MVP).
//
// Output mode `out center` returns (lat, lon) for every node AND ways /
// relations (Overpass synthesises a centroid for non-node geometries),
// which matters because some Transilien stations are tagged as a
// relation (the whole "ensemble gare") rather than a single node.
func FranceTransitOverpassQL(bbox string) string {
	if bbox == "" {
		bbox = FranceMetropolitanBBox
	}
	// Square-bracket statements MUST come first per Overpass QL grammar.
	// The chained union (parentheses) emits one element per matching
	// node / way / relation. `out center tags;` asks for the
	// representative coordinate AND the tag dictionary (needed by the
	// parser to classify type + lines).
	header := "[out:json][timeout:" + itoa(OverpassTimeoutSeconds) +
		"][maxsize:" + itoa(OverpassMaxSizeMB*1024*1024) + "][bbox:" + bbox + "];"
	body := `
(
  node["railway"="station"];
  way["railway"="station"];
  relation["railway"="station"];

  node["railway"="halt"];
  way["railway"="halt"];

  node["railway"="tram_stop"];

  node["public_transport"="station"]["train"="yes"];
  node["public_transport"="station"]["subway"="yes"];
  node["public_transport"="station"]["light_rail"="yes"];
  way["public_transport"="station"]["train"="yes"];
  way["public_transport"="station"]["subway"="yes"];
  relation["public_transport"="station"]["train"="yes"];
);
out center tags;`
	return header + body
}

// FranceTransitRoutesOverpassQL is the companion query that enumerates
// the route relations (métro lines, RER lines, tram lines, train lines)
// AND the public_transport=stop_area relations sitting in the same
// bbox. Issued AFTER FranceTransitOverpassQL so the parser can attach
// the route `ref` ("8", "RER E", "T3a"…) to every station node — most
// stations carry no `ref`/`route_ref` tag of their own, the ref lives on
// the parent route relation.
//
// Why two queries instead of one : asking Overpass to recurse from a
// route into all its members AND from a stop_area into all its members
// blows the response size past 50 MB per dept on populous bboxes
// (Lyon: 12 MB of route geometries we don't need). Splitting keeps
// each sub-query under 5 MB.
//
// What we collect :
//
//   - relation[type=route][route=subway|light_rail|tram|train] — the
//     route definitions with `ref` carrying the line letter / number.
//   - relation[public_transport=stop_area] — the umbrella relation
//     that groups a station node with its stop_position children,
//     letting us bridge "station node X belongs to ligne 4" through
//     a shared stop_area even when X is not a direct route member
//     (Lyon TCL, Marseille RTM, Lille tag schema).
//
// `out body` returns tags AND members, which the parser needs.
func FranceTransitRoutesOverpassQL(bbox string) string {
	if bbox == "" {
		bbox = FranceMetropolitanBBox
	}
	header := "[out:json][timeout:" + itoa(OverpassTimeoutSeconds) +
		"][maxsize:" + itoa(OverpassMaxSizeMB*1024*1024) + "][bbox:" + bbox + "];"
	body := `
(
  relation["type"="route"]["route"="subway"];
  relation["type"="route"]["route"="light_rail"];
  relation["type"="route"]["route"="tram"];
  relation["type"="route"]["route"="train"];
  relation["public_transport"="stop_area"];
);
out body;`
	return header + body
}

// itoa is a tiny private helper to avoid importing strconv just for a
// constant interpolation. Avoids the temptation to fmt.Sprintf which
// would also work but feels heavy for a constant.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

package osm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TransitType enumerates the modes we surface to the UI. The four
// values map to the four user-visible emoji prefixes :
//
//	metro       → 🚇  (Paris M1..M14, Lyon, Marseille, Lille…)
//	rer         → 🚆  (RER A..E in Île-de-France)
//	transilien  → 🚂  (other commuter rail, SNCF Transilien lines)
//	tram        → 🚊  (tramways in every French metropolis)
//
// "train" is the bucket every non-RER non-Transilien railway station
// lands in (TGV gare, Intercités gare, etc.) — same emoji as
// transilien for now ; we may split later if the operator wants.
type TransitType string

// TransitType constants. Persisted verbatim in `nearest_transit_type`
// on the auctions row, so any rename here requires a doctor backfill.
const (
	TransitTypeMetro      TransitType = "metro"
	TransitTypeRER        TransitType = "rer"
	TransitTypeTransilien TransitType = "transilien"
	TransitTypeTrain      TransitType = "train"
	TransitTypeTram       TransitType = "tram"
)

// Station is one entry in the in-memory catalog : a lat/lon plus
// classification metadata derived from the raw Overpass tag set.
//
// Lines is the parsed (and de-duplicated) list of route refs we
// recognised — métro "1", "8", "14" ; RER "A", "B" ; tram "T3a", "T7".
// It can be empty for stations OSM-tagged without a `route_ref` /
// `network` hint, which is common for the smaller Transilien halts.
type Station struct {
	OSMType string      `json:"osm_type"` // "node" / "way" / "relation"
	OSMID   int64       `json:"osm_id"`
	Lat     float64     `json:"lat"`
	Lon     float64     `json:"lon"`
	Name    string      `json:"name"`
	Type    TransitType `json:"type"`
	Lines   []string    `json:"lines,omitempty"`
}

// Display formats the station for the auction-detail UI :
// "Lourmel (M8)" if lines are known, "Lourmel" otherwise. Pure helper,
// no allocation when Lines is empty.
func (s Station) Display() string {
	if len(s.Lines) == 0 {
		return s.Name
	}
	lt := strings.ToUpper(string(s.Type))
	prefix := ""
	switch s.Type {
	case TransitTypeMetro:
		prefix = "M"
	case TransitTypeRER:
		prefix = "RER "
	case TransitTypeTram:
		prefix = "T"
	default:
		prefix = lt[:1]
	}
	if len(s.Lines) == 1 {
		return s.Name + " (" + prefix + s.Lines[0] + ")"
	}
	return s.Name + " (" + prefix + strings.Join(s.Lines, "/") + ")"
}

// overpassResponse mirrors the shape Overpass returns from
// `[out:json]`. Only the fields we need are unmarshalled — the API
// includes a top-level `generator`, `osm3s`, `version` block we ignore.
type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

type overpassElement struct {
	Type    string            `json:"type"` // "node" | "way" | "relation"
	ID      int64             `json:"id"`
	Lat     *float64          `json:"lat,omitempty"`    // present on nodes
	Lon     *float64          `json:"lon,omitempty"`    // present on nodes
	Center  *overpassCenter   `json:"center,omitempty"` // synthesized for ways/relations
	Tags    map[string]string `json:"tags"`
	Members []overpassMember  `json:"members,omitempty"` // present on relations with `out body`
}

// overpassMember mirrors the relation member shape emitted by Overpass
// when `out body` (vs `out tags`) is requested. Roles we care about :
// "stop", "stop_entry_only", "stop_exit_only", "platform" — the route
// stop nodes ; and "" — typical for stop_area members. Other roles
// ("forward", "backward", routing path ways) are ignored.
type overpassMember struct {
	Type string `json:"type"` // "node" | "way" | "relation"
	Ref  int64  `json:"ref"`
	Role string `json:"role"`
}

type overpassCenter struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// ParseOverpass decodes a raw Overpass JSON body and returns the
// filtered, classified station list.
//
// Stations are excluded when ANY of the following holds :
//
//   - lat / lon cannot be resolved (no node coords, no `center`) ;
//   - the element is a disused / abandoned / ghost station ;
//   - the element is a bus-only stop (railway=bus_stop, or a
//     public_transport=platform with bus=yes and no rail tag) ;
//   - the element has no usable `name` tag (we can't render anything
//     meaningful — operator UX requirement).
//
// Returns an error only on JSON parse failure ; an empty stations slice
// is a valid, non-error outcome (e.g. testdata with only bus stops).
func ParseOverpass(body []byte) ([]Station, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("osm: empty body")
	}
	var raw overpassResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("osm: parse: %w", err)
	}
	out := make([]Station, 0, len(raw.Elements))
	for _, el := range raw.Elements {
		st, ok := classifyElement(el)
		if !ok {
			continue
		}
		out = append(out, st)
	}
	// Stable order: by name then OSMID — makes the on-disk catalog
	// deterministic so a refresh that doesn't add/remove stations
	// produces an identical JSON file and the test fixtures stay
	// reviewable.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].OSMID < out[j].OSMID
	})
	return out, nil
}

// classifyElement runs the exclusion rules then derives a Station
// struct. The (Station, bool) return is the same idiom as Go's
// map-lookup convention : ok=false means "drop this element".
func classifyElement(el overpassElement) (Station, bool) {
	if el.Tags == nil {
		return Station{}, false
	}

	// Exclusion : explicit ghost markers.
	if isGhostStation(el.Tags) {
		return Station{}, false
	}

	// Exclusion : bus-only (no rail tag at all).
	if isBusOnly(el.Tags) {
		return Station{}, false
	}

	lat, lon, ok := coordinates(el)
	if !ok {
		return Station{}, false
	}

	name := strings.TrimSpace(el.Tags["name"])
	if name == "" {
		return Station{}, false
	}

	tt, accept := classifyType(el.Tags)
	if !accept {
		return Station{}, false
	}

	return Station{
		OSMType: el.Type,
		OSMID:   el.ID,
		Lat:     lat,
		Lon:     lon,
		Name:    name,
		Type:    tt,
		Lines:   parseLines(el.Tags),
	}, true
}

// isGhostStation flags entries whose lifecycle prefix says "no longer
// in service". OSM contributors use `disused:`, `abandoned:`,
// `razed:`, `demolished:` to mark these — we drop the whole element
// when *any* prefix appears on the railway / station tag.
func isGhostStation(tags map[string]string) bool {
	// Direct tags.
	if tags["disused"] == "yes" || tags["abandoned"] == "yes" {
		return true
	}
	for k := range tags {
		switch {
		case strings.HasPrefix(k, "disused:railway"),
			strings.HasPrefix(k, "disused:public_transport"),
			strings.HasPrefix(k, "abandoned:railway"),
			strings.HasPrefix(k, "abandoned:public_transport"),
			strings.HasPrefix(k, "razed:"),
			strings.HasPrefix(k, "demolished:"):
			return true
		}
	}
	// Some contributors use `station:disused=yes`.
	if tags["station:disused"] == "yes" {
		return true
	}
	return false
}

// isBusOnly returns true when the element is exclusively a bus stop
// with no rail flavour. The Overpass query already filters most bus
// stops out, but defence-in-depth : a future contributor adding a
// bus-related tag to a railway element should not poison the catalog.
//
// Rule : `railway=bus_stop` is always dropped. A
// `public_transport=platform` / `public_transport=stop_position` with
// `bus=yes` and NO `subway/light_rail/train/tram` tag is dropped.
// Any element with a rail mode tag set to "yes" survives this check
// (classifyType will handle the type assignment).
func isBusOnly(tags map[string]string) bool {
	if tags["railway"] == "bus_stop" {
		return true
	}
	if tags["highway"] == "bus_stop" {
		return true
	}
	// public_transport=platform / stop_position with ONLY bus=yes.
	if tags["bus"] == "yes" {
		if tags["subway"] == "yes" || tags["light_rail"] == "yes" ||
			tags["train"] == "yes" || tags["tram"] == "yes" {
			return false
		}
		// railway=station|halt|tram_stop suggests rail, keep it.
		switch tags["railway"] {
		case "station", "halt", "tram_stop":
			return false
		}
		return true
	}
	return false
}

// coordinates returns the (lat, lon) for an Overpass element. Nodes
// carry coords directly ; ways and relations carry them under
// `.center` (synthesised by Overpass when the QL asked for
// `out center`).
func coordinates(el overpassElement) (lat, lon float64, ok bool) {
	if el.Lat != nil && el.Lon != nil {
		return *el.Lat, *el.Lon, true
	}
	if el.Center != nil {
		return el.Center.Lat, el.Center.Lon, true
	}
	return 0, 0, false
}

// classifyType decides which TransitType bucket the station lands in.
// Returns (type, true) when at least one rail mode is detected, or
// (type, false) when the tags don't describe any of our target modes —
// the latter happens e.g. on a `railway=station` with no station=*
// sub-tag AND no mode hint (very rare on French data, but defensive).
func classifyType(tags map[string]string) (TransitType, bool) {
	// Tramway is the easiest call.
	if tags["railway"] == "tram_stop" || tags["tram"] == "yes" {
		return TransitTypeTram, true
	}

	// `station=subway` ⇒ métro. `station=light_rail` ⇒ tram in some
	// French data, métro elsewhere ; we map to métro because the
	// Paris / Lyon / Lille light_rail tags ARE the métros, and tramway
	// is already caught by railway=tram_stop above.
	switch tags["station"] {
	case "subway":
		return TransitTypeMetro, true
	case "light_rail":
		return TransitTypeMetro, true
	}

	// Mode flags. Subway > light_rail > train precedence (Châtelet has
	// all three ; the operator wants the highest-frequency mode shown).
	if tags["subway"] == "yes" {
		return TransitTypeMetro, true
	}
	if tags["light_rail"] == "yes" {
		return TransitTypeMetro, true
	}

	// RER : we detect via `network` or `route_ref` containing "RER".
	// Overpass tags this on the station node itself for Île-de-France
	// data (e.g. Châtelet-Les Halles : network=Réseau express régional).
	netw := strings.ToLower(tags["network"])
	rref := strings.ToUpper(tags["route_ref"])
	if strings.Contains(netw, "rer") || strings.Contains(rref, "RER") {
		return TransitTypeRER, true
	}
	if tags["train"] == "yes" {
		// Transilien (commuter rail) or Intercités / TER mainline.
		if strings.Contains(netw, "transilien") {
			return TransitTypeTransilien, true
		}
		return TransitTypeTrain, true
	}

	// railway=station / halt without a mode sub-tag ⇒ generic train.
	switch tags["railway"] {
	case "station", "halt":
		if strings.Contains(netw, "transilien") {
			return TransitTypeTransilien, true
		}
		return TransitTypeTrain, true
	}

	return "", false
}

// parseLines extracts the route reference list from common OSM tags.
//
//	ref=8                 → ["8"]            (Paris métro line 8)
//	ref="3a;3b"           → ["3a", "3b"]     (Strasbourg tram)
//	route_ref="A;B"       → ["A", "B"]       (RER intersection)
//	line=14               → ["14"]
//
// Returns an empty slice when no recognised tag carries a value — that
// is normal on Transilien halts where OSM rarely lists the SNCF letter.
func parseLines(tags map[string]string) []string {
	candidates := []string{
		tags["ref"], tags["route_ref"], tags["line"],
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	for _, raw := range candidates {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Standard OSM list separator is ";".
		for part := range strings.SplitSeq(raw, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, dup := seen[part]; dup {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

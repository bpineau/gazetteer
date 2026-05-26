package osm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Route is a parsed Overpass `relation[type=route]` element : the
// signature of one métro / RER / tram / train line, plus the IDs of
// its stop / platform member nodes (the nodes a passenger boards at).
//
// `Ref` is the user-visible line reference taken from the route's
// `ref` tag : "8" for Paris métro ligne 8, "A" for Lyon métro A,
// "RER E" for the deprecated alpha-only tagging, "T3a" for tram T3a,
// etc. Empty when the OSM relation has no `ref` (very rare).
//
// `Mode` is normalised to {"subway","light_rail","tram","train"} —
// the route=* value as-is. We use it to filter the lines applied to
// each station by transit type (a métro station only gets subway /
// light_rail lines, never a train ref that happens to pass nearby).
type Route struct {
	ID      int64
	Mode    string  // route=subway|light_rail|tram|train
	Ref     string  // line reference, e.g. "8", "A", "RER E", "T3a"
	Network string  // operator network (used as a tie-break fallback for ref)
	Stops   []int64 // member node IDs with stop-flavour roles
}

// StopArea is a parsed Overpass `relation[public_transport=stop_area]`
// element : it groups one logical station (the user-visible name) with
// its individual `stop_position` nodes (the technical members route
// relations actually reference). Required because in many French
// agglomerations (Lyon TCL, Marseille RTM, Lille Transpole) the
// `railway=station` node is NEVER a direct route member — only the
// `public_transport=stop_position` children are. To attach a line to
// the station we must traverse station ← stop_area → stop_position →
// route.
//
// `Nodes` is the union of every node member regardless of role
// ("stop", "platform", "" — empty is common). We don't filter by role
// here because the catalog station may sit under any of them depending
// on the contributor's habits.
type StopArea struct {
	ID    int64
	Nodes []int64
}

// ParseOverpassRoutes decodes the body of a FranceTransitRoutesOverpassQL
// response and returns the parsed routes + stop_areas. Routes without a
// recognised route=* mode or without a non-empty ref are dropped (they
// can't contribute a UI-visible line label). Stop_areas with no node
// members are dropped (nothing to link to).
//
// Returns an error only on JSON parse failure.
func ParseOverpassRoutes(body []byte) ([]Route, []StopArea, error) {
	if len(body) == 0 {
		return nil, nil, fmt.Errorf("osm: empty body")
	}
	var raw overpassResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("osm: parse routes: %w", err)
	}
	routes := make([]Route, 0, 64)
	stopAreas := make([]StopArea, 0, 64)
	for _, el := range raw.Elements {
		if el.Type != "relation" || el.Tags == nil {
			continue
		}
		switch {
		case el.Tags["type"] == "route":
			r := classifyRoute(el)
			if r == nil {
				continue
			}
			routes = append(routes, *r)
		case el.Tags["public_transport"] == "stop_area":
			sa := classifyStopArea(el)
			if sa == nil {
				continue
			}
			stopAreas = append(stopAreas, *sa)
		}
	}
	return routes, stopAreas, nil
}

// classifyRoute builds a Route from a relation element. Returns nil
// when the relation cannot contribute a usable line label.
func classifyRoute(el overpassElement) *Route {
	mode := el.Tags["route"]
	switch mode {
	case "subway", "light_rail", "tram", "train":
		// accepted
	default:
		return nil
	}
	ref := strings.TrimSpace(el.Tags["ref"])
	if ref == "" {
		// Some OSM contributors put the line reference in `short_name`
		// (Paris: short_name="M8") — fall back to that, stripping the
		// usual one-letter mode prefix so the ref reads as just "8".
		short := strings.TrimSpace(el.Tags["short_name"])
		if short != "" {
			ref = strings.TrimPrefix(short, "M")
			ref = strings.TrimPrefix(ref, "T")
		}
	}
	if ref == "" {
		return nil
	}
	stops := make([]int64, 0, len(el.Members))
	for _, m := range el.Members {
		if m.Type != "node" {
			continue
		}
		switch m.Role {
		case "stop", "stop_entry_only", "stop_exit_only", "stop_forward", "stop_backward",
			"platform", "platform_entry_only", "platform_exit_only",
			"platform_forward", "platform_backward":
			stops = append(stops, m.Ref)
		}
	}
	return &Route{
		ID:      el.ID,
		Mode:    mode,
		Ref:     ref,
		Network: el.Tags["network"],
		Stops:   stops,
	}
}

// classifyStopArea builds a StopArea from a relation element. Returns
// nil when the relation has no node members (nothing to link).
func classifyStopArea(el overpassElement) *StopArea {
	nodes := make([]int64, 0, len(el.Members))
	for _, m := range el.Members {
		if m.Type != "node" {
			continue
		}
		nodes = append(nodes, m.Ref)
	}
	if len(nodes) == 0 {
		return nil
	}
	return &StopArea{ID: el.ID, Nodes: nodes}
}

// modesForType returns the set of OSM route=* values that should be
// considered when populating Lines for a station of the given catalog
// TransitType. A métro station gets subway / light_rail refs ; a tram
// station gets tram ; RER / Transilien / Train share the "train" mode.
// This keeps `lines` on-topic — a tram halt 30 m from a métro station
// (Châtelet) does not inherit the métro number.
func modesForType(tt TransitType) map[string]struct{} {
	switch tt {
	case TransitTypeMetro:
		return map[string]struct{}{"subway": {}, "light_rail": {}}
	case TransitTypeTram:
		return map[string]struct{}{"tram": {}}
	case TransitTypeRER, TransitTypeTransilien, TransitTypeTrain:
		return map[string]struct{}{"train": {}}
	}
	return nil
}

// AttachLinesFromRoutes mutates the input station slice in-place,
// populating Station.Lines from the parsed routes + stop_areas.
//
// Algorithm :
//
//  1. Build `direct[node_id] = map[mode]map[ref]struct{}` from every
//     route's stop members (Paris pattern : the station node IS a
//     direct stop member of the route).
//  2. Build `area[stop_area_id] = map[mode]map[ref]struct{}` by
//     unioning every direct[node] for nodes in that stop_area
//     (Lyon pattern : the station node is in the stop_area but only
//     the stop_position node is in the route).
//  3. Reverse-index every stop_area : for each node member, record
//     the parent stop_area IDs.
//  4. For each station S whose Lines are empty, look up direct[S.ID]
//     first ; on miss, union area[stop_area] across every stop_area
//     that contains S as a node member. Filter by modesForType(S.Type).
//
// A station that already carries a non-empty Lines slice (rare —
// directly tagged via `ref=` / `route_ref=`) is left untouched : the
// node-level tag is the contributor's explicit intent and takes
// precedence over the inferred route membership.
func AttachLinesFromRoutes(stations []Station, routes []Route, stopAreas []StopArea) {
	if len(stations) == 0 || (len(routes) == 0 && len(stopAreas) == 0) {
		return
	}

	// 1. direct[node_id][mode] = set of refs.
	direct := make(map[int64]map[string]map[string]struct{}, len(stations))
	for _, r := range routes {
		for _, nodeID := range r.Stops {
			modeMap, ok := direct[nodeID]
			if !ok {
				modeMap = make(map[string]map[string]struct{}, 2)
				direct[nodeID] = modeMap
			}
			refSet, ok := modeMap[r.Mode]
			if !ok {
				refSet = make(map[string]struct{}, 2)
				modeMap[r.Mode] = refSet
			}
			refSet[r.Ref] = struct{}{}
		}
	}

	// 2. area[stop_area_id][mode] = set of refs, unioning direct hits
	//    on every node member of that stop_area.
	area := make(map[int64]map[string]map[string]struct{}, len(stopAreas))
	for _, sa := range stopAreas {
		var merged map[string]map[string]struct{}
		for _, nodeID := range sa.Nodes {
			d := direct[nodeID]
			if len(d) == 0 {
				continue
			}
			if merged == nil {
				merged = make(map[string]map[string]struct{}, 2)
			}
			for mode, refs := range d {
				target, ok := merged[mode]
				if !ok {
					target = make(map[string]struct{}, len(refs))
					merged[mode] = target
				}
				for ref := range refs {
					target[ref] = struct{}{}
				}
			}
		}
		if merged != nil {
			area[sa.ID] = merged
		}
	}

	// 3. Reverse-index : node_id → list of parent stop_area IDs.
	parentAreas := make(map[int64][]int64, len(stations))
	for _, sa := range stopAreas {
		if _, ok := area[sa.ID]; !ok {
			continue // empty area, skip
		}
		for _, nodeID := range sa.Nodes {
			parentAreas[nodeID] = append(parentAreas[nodeID], sa.ID)
		}
	}

	// 4. Fill in Lines on stations that have none.
	for i := range stations {
		st := &stations[i]
		if len(st.Lines) > 0 {
			continue
		}
		// Only nodes can be a route member ; way/relation stations
		// rely on stop_area traversal, which requires a node ID match
		// (and ways/relations don't appear in our `parentAreas` map
		// keyed by node ID — so they correctly skip).
		modeFilter := modesForType(st.Type)
		if modeFilter == nil {
			continue
		}
		collected := make(map[string]struct{}, 2)
		// Direct membership first.
		if d := direct[st.OSMID]; d != nil {
			for mode, refs := range d {
				if _, ok := modeFilter[mode]; !ok {
					continue
				}
				for ref := range refs {
					collected[ref] = struct{}{}
				}
			}
		}
		// Stop_area-mediated membership.
		for _, areaID := range parentAreas[st.OSMID] {
			a := area[areaID]
			for mode, refs := range a {
				if _, ok := modeFilter[mode]; !ok {
					continue
				}
				for ref := range refs {
					collected[ref] = struct{}{}
				}
			}
		}
		if len(collected) == 0 {
			continue
		}
		// Stable order so successive refreshes produce a byte-identical
		// on-disk catalog when OSM didn't change.
		lines := make([]string, 0, len(collected))
		for ref := range collected {
			lines = append(lines, ref)
		}
		sort.Strings(lines)
		st.Lines = lines
	}
}

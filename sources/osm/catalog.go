package osm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bpineau/gazetteer/helpers/atomicfs"
)

// CatalogSchemaVersion is bumped whenever the on-disk JSON layout
// changes. Mismatched files are treated as a cache miss (next refresh
// rewrites them). v2 adds the per-station `lines` slice populated by
// AttachLinesFromRoutes — pre-v2 snapshots predate route_master/lines
// extraction and must be discarded so the next refresh repopulates them.
const CatalogSchemaVersion = 2

// RefreshAfter is the cache lifetime for the on-disk catalog. The OSM
// data is updated continuously, but new métro / RER stations are rare
// (one or two per year nationwide) and a monthly refresh comfortably
// captures that churn. The MVP requirement is "1× per month max".
const RefreshAfter = 30 * 24 * time.Hour

// Catalog is the in-memory snapshot of every train-class station in
// metropolitan France, loaded once at process start. Lookups are
// O(N) — N≈9 k at the time of writing — which is sub-millisecond on
// modern hardware and well within the per-auction budget.
type Catalog struct {
	SchemaVersion int       `json:"schema_version"`
	FetchedAt     time.Time `json:"fetched_at"`
	BBox          string    `json:"bbox"`
	Stations      []Station `json:"stations"`
}

// IsFresh reports whether the catalog is younger than RefreshAfter and
// has at least one station — the two preconditions the refresh
// scheduler uses to skip a network round-trip.
func (c *Catalog) IsFresh(now time.Time) bool {
	if c == nil || len(c.Stations) == 0 {
		return false
	}
	return now.Sub(c.FetchedAt) < RefreshAfter
}

// NearestStation returns the station closest to (lat, lon) by
// great-circle distance, the haversine value in metres, and the
// derived walking distance in metres (sinuosity-scaled). Returns
// (nil, 0, 0) when the catalog is empty or the input coordinates are
// the (0, 0) sentinel.
//
// Equivalent to NearestStationWithinMeters(lat, lon, 0) — no proximity
// cap — kept for back-compat and tests. Production callers should use
// NearestStationWithinMeters so DOM-TOM auctions and remote rural
// points stop matching catalog stations thousands of kilometres away.
//
// Linear scan — kept naive on purpose for MVP. A k-d-tree shaves the
// per-call cost from tens of µs to a few µs but at N≈9 000 stations ×
// M≈8 000 auctions the total enrich-run cost stays well under a second,
// invisible next to the SQL+JSON round-trips. Revisit when M crosses
// 100 000.
func (c *Catalog) NearestStation(lat, lon float64) (st *Station, haversineMeters float64, walkMeters int) {
	return c.NearestStationWithinMeters(lat, lon, 0)
}

// NearestStationWithinMeters returns the catalog station closest to
// (lat, lon) provided its great-circle distance is ≤ maxHaversineMeters.
// A non-positive cap disables the bound (back-compat with the original
// NearestStation API). When even the nearest station is past the cap
// — what happens for DOM-TOM auctions matched against a metropolitan-
// France catalog, or for remote rural points hundreds of km from any
// rail station — returns (nil, 0, 0). The caller is expected to treat
// that as "no walkable station nearby" and persist NULL transit columns.
//
// Returns (nil, 0, 0) on an empty/nil catalog or the (0, 0) sentinel.
func (c *Catalog) NearestStationWithinMeters(lat, lon float64, maxHaversineMeters float64) (st *Station, haversineMeters float64, walkMeters int) {
	if c == nil || len(c.Stations) == 0 {
		return nil, 0, 0
	}
	if lat == 0 && lon == 0 {
		return nil, 0, 0
	}
	bestIdx := -1
	bestDist := -1.0
	for i := range c.Stations {
		d := HaversineMeters(lat, lon, c.Stations[i].Lat, c.Stations[i].Lon)
		if bestIdx == -1 || d < bestDist {
			bestIdx = i
			bestDist = d
		}
	}
	if bestIdx == -1 {
		return nil, 0, 0
	}
	if maxHaversineMeters > 0 && bestDist > maxHaversineMeters {
		// The catalog covers metropolitan France; this auction is too
		// far from any station to be walkable. Refuse the match rather
		// than silently output a 19 920 km "walking distance".
		return nil, 0, 0
	}
	st = &c.Stations[bestIdx]
	return st, bestDist, WalkingMetersFromHaversine(bestDist)
}

// LoadCatalog reads a catalog file from disk. Missing file or
// schema-version mismatch returns a nil Catalog with no error — the
// caller treats that as "needs refresh", matches the
// "miss-not-an-error" idiom used elsewhere in the repo.
func LoadCatalog(path string) (*Catalog, error) {
	if path == "" {
		return nil, nil //nolint:nilnil
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is a controlled cache file location, not user input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil //nolint:nilnil
		}
		return nil, fmt.Errorf("osm: read catalog %q: %w", path, err)
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("osm: parse catalog %q: %w", path, err)
	}
	if c.SchemaVersion != CatalogSchemaVersion {
		// Treat as a cold miss : the format moved on.
		return nil, nil //nolint:nilnil
	}
	return &c, nil
}

// SaveCatalog persists a catalog to disk atomically (write to a tmp
// file in the same directory, then rename — ensures concurrent readers
// never observe a half-written file even under SIGTERM). Creates the
// parent directory on demand.
func SaveCatalog(path string, c *Catalog) error {
	if path == "" {
		return errors.New("osm: SaveCatalog: empty path")
	}
	if c == nil {
		return errors.New("osm: SaveCatalog: nil catalog")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // public cache dir; no secrets
		return fmt.Errorf("osm: mkdir for %q: %w", path, err)
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("osm: marshal catalog: %w", err)
	}
	if err := atomicfs.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("osm: %w", err)
	}
	return nil
}

// DefaultCatalogPath returns the canonical on-disk catalog path under
// the operator's --data-dir. Returns "" when dataDir is empty so the
// caller can fall back to an in-memory-only mode (useful for tests).
func DefaultCatalogPath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "osm", "transit_stations.json")
}

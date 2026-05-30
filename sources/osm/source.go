package osm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "osm_transit"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
//
// Version 3 : Station.Lines is now populated by joining the parent
// route relations (`relation[type=route][route=*]`) and stop_area
// umbrellas. Previously the catalog only kept the `ref` / `route_ref`
// tag carried directly on the station node, which was empty for ~89 %
// of stations.
const sourceVersion = 3

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// MaxNearestStationMeters caps the haversine distance the OSM transit
// Source tolerates between a listing and the closest catalog station.
// 5 000 m great-circle (~6 500 m walked at the canonical 1.3 sinuosity
// multiplier, ~80 min at 80 m/min) is well past any genuine "à pied"
// use case but generous enough to keep central-distribution matches
// (p50 ≈ 850 m, p90 ≈ 3 315 m) untouched. Above the cap the Source
// refuses the match and returns a Result with SkipReason = OutOfRange.
const MaxNearestStationMeters = 5000.0

// ErrNoCatalog is returned by Query when the Source was constructed
// without a usable catalog (or one was supplied but is empty). Treated
// as a transient blocker by stateful callers — once the catalog is
// loaded via UpdateCatalog the next call succeeds.
var ErrNoCatalog = errors.New("osm: catalog not loaded")

// Options configures an osm Source. The zero value is usable: every
// field has a sane default. Catalog may be nil — the Source then
// returns ErrNoCatalog until UpdateCatalog is called with a non-empty
// catalog (typically by a background refresh goroutine).
type Options struct {
	// Catalog overrides the station catalog. When nil, NewSource loads it
	// via Load(DataDir) — a refreshed copy in the datadir takes precedence
	// over the embedded baseline. Tests inject a stub here.
	Catalog *Catalog

	// DataDir is the gazetteer data directory. A refreshed catalog found
	// there overrides the embedded baseline. Empty means embedded-only.
	DataDir string

	// Fetcher enables the live Overpass fallback: when the catalog has no
	// station within range of a query point (a zone the baseline does not
	// cover, or no catalog at all), the Source queries Overpass live around
	// that point. Nil disables the fallback (catalog-only).
	Fetcher OverpassFetcher
}

// Source implements gazetteer.Source for the OSM transit enricher. Use
// NewSource to construct. Concurrency-safe: the catalog pointer is
// updated atomically, so a refresh can hot-swap it while Query calls are in
// flight.
type Source struct {
	catalog atomic.Pointer[Catalog]
	fetcher OverpassFetcher
}

// NewSource builds an osm Source. Zero-valued Options is fine: the embedded
// baseline catalog is loaded automatically. With a Fetcher set, queries the
// catalog can't answer fall back to a live Overpass lookup.
func NewSource(opts Options) *Source {
	s := &Source{fetcher: opts.Fetcher}
	cat := opts.Catalog
	if cat == nil {
		// Embedded baseline / datadir override. Errors are rare (a corrupt
		// committed embed); the live fallback covers a missing catalog.
		cat, _ = Load(opts.DataDir)
	}
	if cat != nil && len(cat.Stations) > 0 {
		s.catalog.Store(cat)
	}
	return s
}

// Datasets implements gazetteer.DatasetProvider, exposing the station
// catalog to the dataset refresh tooling (rebuilt from a live Overpass
// refresh — see transform).
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// UpdateCatalog atomically replaces the Source's station catalog.
// Safe to call from any goroutine while Query is running. A nil or
// empty catalog is ignored so a failed background refresh cannot
// silently discard an already-loaded one.
func (s *Source) UpdateCatalog(c *Catalog) {
	if c == nil || len(c.Stations) == 0 {
		return
	}
	s.catalog.Store(c)
}

// Catalog returns the currently-installed catalog snapshot, or nil
// when none has been installed. Exposed for tests and for downstream
// consumers that want to report catalog stats.
func (s *Source) Catalog() *Catalog {
	return s.catalog.Load()
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It looks up the closest catalog
// station to the listing's (Lat, Lon) and returns a *Result.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing Listing.Lat or Listing.Lon, or both equal to 0 (the
//     "unset coords" sentinel) → gazetteer.ErrInsufficientInputs (wrapped).
//   - Catalog absent or empty → ErrNoCatalog (transient: next Query
//     after UpdateCatalog will succeed).
//   - Successful but no station within MaxNearestStationMeters → a
//     non-nil *Result with IsEmpty() == true and
//     SkipReason == SkipReasonOutOfRange. The framework records
//     StatusOKEmpty; downstream consumers can use SkipReason to map
//     this to a permanent skip.
//   - Successful pick → *Result with SampleSize==1 + a populated
//     Evidence sidecar.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "osm_transit" component. Wrappers
// that batch many queries typically log a single INFO line per
// work-unit.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("osm: %w: missing lat/lon", gazetteer.ErrInsufficientInputs)
	}
	lat := *l.Lat
	lon := *l.Lon
	if lat == 0 && lon == 0 {
		return nil, fmt.Errorf("osm: %w: lat/lon=0,0 sentinel", gazetteer.ErrInsufficientInputs)
	}

	cat := s.catalog.Load()
	hasCat := cat != nil && len(cat.Stations) > 0

	catStations := 0
	var catFetchedAt time.Time
	if hasCat {
		catStations, catFetchedAt = len(cat.Stations), cat.FetchedAt
	}
	mkEvidence := func(haversine float64, stations int, fetchedAt time.Time) Evidence {
		return Evidence{
			AuctionLat:       lat,
			AuctionLon:       lon,
			HaversineMeters:  int(haversine),
			WalkMultiplier:   WalkSinuosityMultiplier,
			ProximityCapM:    MaxNearestStationMeters,
			CatalogFetchedAt: fetchedAt.UTC().Format(time.RFC3339),
			CatalogStations:  stations,
		}
	}
	mkHit := func(st *Station, haversine float64, walkM, stations int, fetchedAt time.Time) *Result {
		return &Result{
			NearestTransitName:    st.Name,
			NearestTransitType:    st.Type,
			NearestTransitLines:   st.Lines,
			NearestTransitWalkM:   walkM,
			NearestTransitWalkMin: WalkMinutes(walkM),
			Confidence:            ConfidenceHigh,
			SampleSize:            1,
			Evidence:              mkEvidence(haversine, stations, fetchedAt),
		}
	}

	// 1. Catalog fast path (offline).
	if hasCat {
		if st, hav, walk := cat.NearestStationWithinMeters(lat, lon, MaxNearestStationMeters); st != nil {
			return mkHit(st, hav, walk, catStations, catFetchedAt), nil
		}
	}

	// 2. Live Overpass fallback — covers points the catalog can't answer (a
	//    zone the baseline doesn't cover, or no catalog at all).
	if s.fetcher != nil {
		st, hav, walk, n, err := s.liveNearest(ctx, lat, lon)
		switch {
		case err != nil:
			logger.Debug("osm.live_fallback_failed", slog.String("err", err.Error()))
			if !hasCat {
				return nil, ErrNoCatalog // transient: no offline data and live failed
			}
		case st != nil:
			logger.Debug("osm.live_hit", slog.String("station", st.Name))
			return mkHit(st, hav, walk, n, time.Now()), nil
		}
	}

	// 3. Nothing within range from either path.
	if !hasCat && s.fetcher == nil {
		return nil, ErrNoCatalog
	}
	logger.Debug("osm.out_of_range",
		slog.Float64("lat", lat),
		slog.Float64("lon", lon),
		slog.Float64("cap_m", MaxNearestStationMeters),
	)
	return &Result{
		Confidence: ConfidenceLow,
		SampleSize: 0,
		Skipped:    true,
		SkipReason: SkipReasonOutOfRange,
		Evidence:   mkEvidence(0, catStations, catFetchedAt),
	}, nil
}

// liveNearest queries Overpass for transit stations in a small bounding box
// around (lat, lon) and returns the nearest one within the proximity cap. It
// reuses the catalog query + parser + nearest-station logic, so a live hit
// is shaped exactly like a catalog hit (minus route lines, which the cheap
// per-point query skips).
func (s *Source) liveNearest(ctx context.Context, lat, lon float64) (st *Station, haversineMeters float64, walkMeters, nStations int, err error) {
	bbox := pointBBox(lat, lon, MaxNearestStationMeters)
	body, err := s.fetcher.Query(ctx, FranceTransitOverpassQL(bbox))
	if err != nil {
		return nil, 0, 0, 0, err
	}
	stations, err := ParseOverpass(body)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	tmp := &Catalog{Stations: stations}
	st, hav, walk := tmp.NearestStationWithinMeters(lat, lon, MaxNearestStationMeters)
	return st, hav, walk, len(stations), nil
}

// pointBBox returns an Overpass "south,west,north,east" bbox covering a
// radiusM-metre square around (lat, lon).
func pointBBox(lat, lon, radiusM float64) string {
	dLat := radiusM / 111_000.0
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat < 0.01 {
		cosLat = 0.01
	}
	dLon := radiusM / (111_000.0 * cosLat)
	return fmt.Sprintf("%.5f,%.5f,%.5f,%.5f", lat-dLat, lon-dLon, lat+dLat, lon+dLon)
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// out-of-range response still returns a non-nil *Result with
// IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("osm: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

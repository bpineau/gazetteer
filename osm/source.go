package osm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/bpineau/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key. Re-exported by
// the encheridor adapter as its enricher name.
const Name = "osm_transit"

// sourceVersion bumps when the Source's internal logic changes. Callers
// (encheridor's runner) gate cache invalidation on it.
//
// Version 3 (2026-05-18) : Station.Lines is now populated by joining
// the parent route relations (`relation[type=route][route=*]`) and
// stop_area umbrellas. Previously the catalog only kept the `ref` /
// `route_ref` tag carried directly on the station node, which was
// empty for ~89 % of stations.
const sourceVersion = 3

// Version exposes sourceVersion so callers that wrap the Source (e.g.
// encheridor's adapter) can mirror it without reaching into the package
// internals.
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
	// Catalog is the initial station catalog. May be nil — call
	// UpdateCatalog later to install one. Empty catalogs are accepted
	// at construction (no error) but every Query returns ErrNoCatalog
	// until a non-empty catalog is installed.
	Catalog *Catalog
}

// Source implements gazetteer.Source for the OSM transit enricher. Use
// NewSource to construct. Concurrency-safe: the catalog pointer is
// updated atomically, so background refresh goroutines can hot-swap it
// while Query calls are in flight.
type Source struct {
	catalog atomic.Pointer[Catalog]
}

// NewSource builds an osm Source. Zero-valued Options is fine. The
// Source registers immediately, even with a nil/empty catalog —
// Query then returns ErrNoCatalog until UpdateCatalog supplies a real
// one. This lets the serve process boot without blocking on Overpass.
func NewSource(opts Options) *Source {
	s := &Source{}
	if opts.Catalog != nil && len(opts.Catalog.Stations) > 0 {
		s.catalog.Store(opts.Catalog)
	}
	return s
}

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
// when none has been installed. Exposed for tests + the encheridor
// adapter (Reads catalog stats for the EnrichPayload.Method.Params).
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
// the table in pkg/gazetteer/source.go):
//
//   - Missing Listing.Lat or Listing.Lon, or both equal to 0 (the
//     "unset coords" sentinel) → gazetteer.ErrInsufficientInputs (wrapped).
//   - Catalog absent or empty → ErrNoCatalog (transient: next Query
//     after UpdateCatalog will succeed).
//   - Successful but no station within MaxNearestStationMeters → a
//     non-nil *Result with IsEmpty() == true and
//     SkipReason == SkipReasonOutOfRange. The framework records
//     StatusOKEmpty; the encheridor adapter consults SkipReason to
//     map this to enrich.ErrPermanentlyOutOfScope.
//   - Successful pick → *Result with SampleSize==1 + a populated
//     Evidence sidecar.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "osm_transit" component. The
// encheridor adapter on top adds INFO once per work-unit.
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
	if cat == nil || len(cat.Stations) == 0 {
		return nil, ErrNoCatalog
	}

	// emptyEvidence pre-fills the sidecar fields known before the lookup
	// outcome (auction coords, catalog stats, multiplier, cap).
	emptyEvidence := func(haversine float64) Evidence {
		return Evidence{
			AuctionLat:       lat,
			AuctionLon:       lon,
			HaversineMeters:  int(haversine),
			WalkMultiplier:   WalkSinuosityMultiplier,
			ProximityCapM:    MaxNearestStationMeters,
			CatalogFetchedAt: cat.FetchedAt.UTC().Format(time.RFC3339),
			CatalogStations:  len(cat.Stations),
		}
	}

	st, haversine, walkM := cat.NearestStationWithinMeters(lat, lon, MaxNearestStationMeters)
	if st == nil {
		// Two sub-cases:
		//  (a) lat/lon is the (0, 0) sentinel — already caught above.
		//      NearestStationWithinMeters returns nil for it but we
		//      don't reach this branch.
		//  (b) the nearest station is beyond the proximity cap. Out
		//      of range — return a sentinel Result the adapter maps
		//      to enrich.ErrPermanentlyOutOfScope.
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
			Evidence:   emptyEvidence(0),
		}, nil
	}

	out := &Result{
		NearestTransitName:    st.Name,
		NearestTransitType:    st.Type,
		NearestTransitLines:   st.Lines,
		NearestTransitWalkM:   walkM,
		NearestTransitWalkMin: WalkMinutes(walkM),
		Confidence:            ConfidenceHigh,
		SampleSize:            1,
		Evidence:              emptyEvidence(haversine),
	}
	return out, nil
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

// From extracts the typed *Result from a Dossier. Returns (nil, false)
// when the source is absent, failed, or the Data does not match.
func From(d gazetteer.Dossier) (*Result, bool) {
	return gazetteer.Get[*Result](d, Name)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

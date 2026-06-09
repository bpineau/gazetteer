package cdsr

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "cdsr"

// sourceVersion bumps when the Source's internal logic changes. Callers gate
// cache invalidation on it.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it
// without reaching into the package internals.
const Version = sourceVersion

// MaxNearestMeters caps the haversine distance at which a CDSR copro is
// considered "near" the listing. Beyond it the Source reports no signal
// (StatusOKEmpty): a labelled distressed copro more than 3 km away says nothing
// useful about the listing's own neighbourhood.
//
// Fixed, not runtime-tunable (exposed for consumers that want to label the
// range). Result.Within3km mirrors this value — keep the two in sync if it ever
// changes.
const MaxNearestMeters = 3000.0

// radius500m is the inner radius counted separately in Result.Within500m.
const radius500m = 500.0

// maxNearestItems caps how many copros Result.Nearest lists.
const maxNearestItems = 5

// Options configures a cdsr Source. The zero value is usable.
type Options struct {
	// Catalog overrides the lazily-loaded singleton. Tests inject a stub here;
	// production callers leave it nil.
	Catalog *Catalog

	// DataDir is the gazetteer data directory. A refreshed copy found there
	// overrides the embedded snapshot. Empty means embedded-only.
	DataDir string
}

// Source implements gazetteer.Source for the CDSR proximity enricher. Use
// NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a cdsr Source. Zero-valued Options is fine: the embedded
// snapshot is loaded automatically.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded snapshot
// to the refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. It needs the listing's coordinates; with
// them it returns the nearest CDSR copros within MaxNearestMeters.
//
// Error mapping:
//   - Missing Listing.Lat/Lon (or both 0) → gazetteer.ErrInsufficientInputs.
//   - No CDSR copro within MaxNearestMeters → a non-nil *Result with
//     IsEmpty() == true (StatusOKEmpty).
//   - Otherwise → a *Result listing the nearby copros.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("cdsr: %w: missing lat/lon", gazetteer.ErrInsufficientInputs)
	}
	lat, lon := *l.Lat, *l.Lon
	if lat == 0 && lon == 0 {
		return nil, fmt.Errorf("cdsr: %w: lat/lon=0,0 sentinel", gazetteer.ErrInsufficientInputs)
	}

	cat := s.opts.Catalog
	if cat == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("cdsr: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		cat = loaded
	}

	ev := Evidence{ListingLat: lat, ListingLon: lon, MaxMeters: MaxNearestMeters, CatalogSize: len(cat.Copros)}

	hits := cat.withinSorted(lat, lon, MaxNearestMeters)
	if len(hits) == 0 {
		return &Result{Evidence: ev}, nil // IsEmpty: no CDSR within range
	}

	res := &Result{
		NearestM:   int(hits[0].meters),
		Within3km:  len(hits),
		Confidence: ConfidenceHigh,
		Evidence:   ev,
	}
	for _, h := range hits {
		if h.meters <= radius500m {
			res.Within500m++
		}
		if len(res.Nearest) < maxNearestItems {
			res.Nearest = append(res.Nearest, Item{
				Name:      h.copro.Name,
				Address:   h.copro.Address,
				Commune:   h.copro.Commune,
				Lots:      h.copro.Lots,
				LabelYear: h.copro.LabelYear,
				DistanceM: int(h.meters),
			})
		}
	}
	logger.Debug("cdsr.hit", slog.Int("within_3km", res.Within3km), slog.Int("nearest_m", res.NearestM))
	return res, nil
}

// Query is the atomic helper for callers who don't want the builder. The error
// is non-nil only when the Source failed; a successful "none nearby" response
// still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}

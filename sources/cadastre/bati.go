package cadastre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// BatiFeatureCollection is the GeoJSON envelope of the per-commune
// PCI building dump. The upstream serves it as
// `application/vnd.geo+json`, gzipped — Go's net/http transparently
// decodes the body so the parser sees raw JSON.
type BatiFeatureCollection struct {
	Features []BatiFeature `json:"features"`
}

// BatiFeature is one building polygon in the dump. Properties carry
// minor metadata (commune, type code, dates) we don't surface.
type BatiFeature struct {
	Geometry RawGeometry `json:"geometry"`
}

// ParseBatiFeatureCollection decodes the building dump body. Returns
// ErrEmptyBody on a nil / unparseable body.
func ParseBatiFeatureCollection(body []byte) (*BatiFeatureCollection, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	fc := &BatiFeatureCollection{}
	if err := json.Unmarshal(body, fc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return fc, nil
}

// LoadBatiPolygons decodes the dump body into the cache-ready
// BatiPolygon slice. Each entry carries the typed MultiPolygon, its
// pre-computed centroid, and its planar area — so the per-parcel
// filter doesn't repeatedly decode + project the same polygons on
// subsequent Query calls hitting the same INSEE. Features whose
// geometry is missing or malformed are silently skipped (the dump
// occasionally carries empty bâti placeholders).
func LoadBatiPolygons(body []byte) ([]BatiPolygon, int, error) {
	fc, err := ParseBatiFeatureCollection(body)
	if err != nil {
		return nil, 0, err
	}
	raw := len(fc.Features)
	out := make([]BatiPolygon, 0, raw)
	for _, f := range fc.Features {
		mp, err := ParsePolygonGeometry(f.Geometry)
		if err != nil || len(mp) == 0 {
			continue
		}
		out = append(out, BatiPolygon{
			Geometry: mp,
			Centroid: mp.Centroid(),
			AreaM2:   mp.AreaM2(),
		})
	}
	return out, raw, nil
}

// fetchBati performs the HTTP GET on the per-commune building dump via
// the shared gazetteer.FetchUpstream helper. 404 → no bâti for this
// commune: mapped onto an empty dump rather than an error so the
// caller can populate Bati* with zeros rather than soft-fail. The HTTP
// client handles gzip transparently.
func (s *Source) fetchBati(ctx context.Context, u string) ([]byte, error) {
	if s.opts.Fetcher != nil {
		return s.opts.Fetcher.Fetch(ctx, u)
	}
	return gazetteer.FetchUpstream(ctx, s.opts.HTTPClient, u, gazetteer.FetchSpec{
		Prefix:       "cadastre: bati",
		Accept:       "application/vnd.geo+json,application/json",
		NotFoundBody: []byte(`{"type":"FeatureCollection","features":[]}`),
	})
}

// applyBatiBaseURL rewrites the upstream root with s.opts.BatiBaseURL
// when set. Mirrors applyBaseURL for the parcelle endpoint.
func (s *Source) applyBatiBaseURL(u string) string {
	if s.opts.BatiBaseURL == "" {
		return u
	}
	return s.opts.BatiBaseURL + strings.TrimPrefix(u, BatiBaseURL)
}

// batiSharedFetchTimeout bounds a coalesced bâti fetch when the initiating
// caller's context carries no deadline of its own. The shared fetch is
// detached from that caller's CANCELLATION (see resolveBatiPolygons), so
// without a bound of its own a hung upstream would pin the goroutine, and the
// several MB of dump it is buffering, for the life of the process.
const batiSharedFetchTimeout = 2 * time.Minute

// batiDump is what one coalesced fetch yields to every waiter on it.
type batiDump struct {
	// polygons is the parsed, cache-ready building footprint set.
	polygons []BatiPolygon
	// rawCount is the number of features the dump carried before the
	// malformed-geometry filter (Evidence.BatiRawCount).
	rawCount int
}

// resolveBatiPolygons returns the per-commune building polygons for
// `insee`, fetching + caching them on miss. The cache is keyed by
// INSEE; cached hits set `cached` to true.
//
// A miss is expensive (a whole commune's PCI dump: a few MB over the wire,
// tens of thousands of polygons to parse), so misses for the same commune
// are COALESCED through s.batiSF: parallel Query calls on nearby addresses
// pay for one download between them, and the last one out reads the cache
// the flight populated. Two properties make that safe:
//
//   - Context isolation. The shared fetch runs on the initiator's context
//     values (HTTP client, logger) with its cancellation detached, so an
//     initiator that gives up cannot cancel the fetch its fellow waiters
//     are still blocked on. It stays BOUNDED: the initiator's own deadline
//     when it has one, else batiSharedFetchTimeout. Every waiter selects on
//     its OWN context, so a cancelled waiter returns immediately.
//
//   - Error fidelity. singleflight hands the (value, error) pair to every
//     waiter, so a failed fetch surfaces AS an error to all of them (each
//     soft-failing per the bâti contract) and is never cached, nor shared
//     as an empty success.
func (s *Source) resolveBatiPolygons(ctx context.Context, insee string) (polys []BatiPolygon, rawCount int, cached bool, queriedURL string, err error) {
	cache := s.opts.BatiCache
	if cache == nil {
		cache = s.defaultCache
	}
	if hit, ok := cache.Get(insee); ok {
		// Raw count is not persisted on the cache — we only know the
		// *filtered* count post-load. Surface the cached slice length
		// as a reasonable approximation (every cached entry is one
		// well-formed building).
		return hit, len(hit), true, "", nil
	}
	rawURL, err := BatimentsURLForINSEE(insee)
	if err != nil {
		return nil, 0, false, "", err
	}
	urlToHit := s.applyBatiBaseURL(rawURL)

	ch := s.batiSF.DoChan(insee, func() (any, error) {
		fetchCtx, cancel := sharedFetchContext(ctx, batiSharedFetchTimeout)
		defer cancel()
		body, err := s.fetchBati(fetchCtx, urlToHit)
		if err != nil {
			return nil, err
		}
		loaded, raw, err := LoadBatiPolygons(body)
		if err != nil {
			return nil, fmt.Errorf("cadastre: bati: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
		}
		cache.Put(insee, loaded)
		return batiDump{polygons: loaded, rawCount: raw}, nil
	})

	select {
	case <-ctx.Done():
		return nil, 0, false, urlToHit, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, 0, false, urlToHit, res.Err
		}
		dump := res.Val.(batiDump)
		return dump.polygons, dump.rawCount, false, urlToHit, nil
	}
}

// sharedFetchContext derives the context a coalesced fetch runs on: the
// initiating caller's VALUES (injected HTTP client, logger) with its
// cancellation detached, bounded by that caller's own deadline when it has
// one and by fallback otherwise. Detaching the cancellation is the point:
// the caller that happens to start a flight must not be able to cancel it
// out from under the waiters that joined it.
func sharedFetchContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, fallback)
}

// filterBatiInParcel walks `polys` and keeps the ones whose centroid
// sits inside `parcel`. Returns the filtered slice (typically much
// smaller than `polys` — the dump has every building on the commune).
//
// `parcel` is the API Carto parcel geometry; using a MultiPolygon
// directly matches what API Carto actually returns and dodges a
// Polygon-vs-MultiPolygon split at the call site.
func filterBatiInParcel(polys []BatiPolygon, parcel geopoly.MultiPolygon) []BatiPolygon {
	if len(polys) == 0 || len(parcel) == 0 {
		return nil
	}
	out := make([]BatiPolygon, 0)
	for _, p := range polys {
		if parcel.Covers(p.Centroid) {
			out = append(out, p)
		}
	}
	return out
}

// sumBatiArea sums the planar area of every cached polygon.
func sumBatiArea(polys []BatiPolygon) float64 {
	var total float64
	for _, p := range polys {
		total += p.AreaM2
	}
	return total
}

// errBatiSkipped is sentinelled to nil — the bâti path NEVER returns
// an error to the caller; it stamps Evidence.BatiError instead. This
// var is retained as a compile-time guard against accidental error
// propagation from a future refactor of the bâti pipeline.
var errBatiSkipped = errors.New("cadastre: bati skipped")

var _ = errBatiSkipped // keep referenced; see comment above

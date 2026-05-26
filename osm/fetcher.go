package osm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/pkg/httpx"
)

// OverpassFetcher abstracts "POST a QL query, get back the body bytes"
// so the catalog refresher is testable without an httpx.Client. The
// production implementation is HTTPOverpassFetcher ; tests inject
// FuncOverpassFetcher.
type OverpassFetcher interface {
	Query(ctx context.Context, ql string) ([]byte, error)
}

// FuncOverpassFetcher adapts a plain function to the OverpassFetcher
// interface — same idiom as encheridor/pkg/circuit.FuncFetcher but
// scoped to this package because Overpass uses POST not GET.
type FuncOverpassFetcher func(ctx context.Context, ql string) ([]byte, error)

// Query satisfies the OverpassFetcher interface.
func (f FuncOverpassFetcher) Query(ctx context.Context, ql string) ([]byte, error) {
	return f(ctx, ql)
}

// HTTPOverpassFetcher posts QL queries against an Overpass endpoint.
// Concurrency-safe : the underlying httpx.Client carries its own
// per-host limiter. When the primary endpoint returns a non-2xx
// response, Query tries each fallback in order and returns the first
// success — making catalog refresh resilient to individual mirror
// outages.
//
// Observability: each per-mirror failure inside the fallback loop is
// emitted at WARN level (`osm.mirror_failed`) so an operator can
// distinguish "primary serving" from "primary down, fallback rescued
// the call". When every mirror fails, the loop emits a single ERROR
// (`osm.all_mirrors_failed`) before propagating the last transport
// error upstream. Without this, the fallback path would mask a
// systemic outage as a transient blip.
type HTTPOverpassFetcher struct {
	http      *httpx.Client
	endpoint  string
	fallbacks []string
	logger    *slog.Logger
}

// NewHTTPOverpassFetcher returns a fetcher bound to c. `endpoint` may
// be empty — it then falls back to the package-level OverpassEndpoint.
// The package-level OverpassFallbackEndpoints list is used automatically.
// The logger defaults to slog.Default() — callers wanting per-test capture
// should set the Logger field directly after construction.
func NewHTTPOverpassFetcher(c *httpx.Client, endpoint string) *HTTPOverpassFetcher {
	if endpoint == "" {
		endpoint = OverpassEndpoint
	}
	return &HTTPOverpassFetcher{
		http:      c,
		endpoint:  endpoint,
		fallbacks: OverpassFallbackEndpoints,
		logger:    slog.Default(),
	}
}

// SetLogger overrides the default slog logger. Used by tests to capture
// the warn/error lines emitted by the fallback loop without polluting
// stderr.
func (f *HTTPOverpassFetcher) SetLogger(l *slog.Logger) {
	if l != nil {
		f.logger = l
	}
}

// Query posts the QL body to the Overpass interpreter and returns the
// raw response bytes. Times out per the caller-supplied context ; the
// Overpass server itself caps individual queries at OverpassTimeoutSeconds.
//
// The interpreter expects the QL string under the `data` form-field —
// not as a JSON body, not as a raw body. We post `application/x-www-form-urlencoded`
// with `data=<urlencoded QL>` so the request matches the protocol the
// public mirrors all serve.
//
// If the primary endpoint returns a non-2xx status, Query retries each
// fallback endpoint in order, returning the first successful response.
// This makes catalog refresh resilient to individual mirror outages.
func (f *HTTPOverpassFetcher) Query(ctx context.Context, ql string) ([]byte, error) {
	if f.http == nil {
		return nil, errors.New("osm: nil http client")
	}
	if strings.TrimSpace(ql) == "" {
		return nil, errors.New("osm: empty QL")
	}

	endpoints := make([]string, 0, 1+len(f.fallbacks))
	endpoints = append(endpoints, f.endpoint)
	endpoints = append(endpoints, f.fallbacks...)

	logger := f.logger
	if logger == nil {
		logger = slog.Default()
	}

	var lastErr error
	for i, ep := range endpoints {
		body, err := f.queryOne(ctx, ep, ql)
		if err == nil {
			return body, nil
		}
		// Surface every mirror failure so a partial outage is observable.
		// Without this, a "fallback rescued" situation looks identical in
		// the logs to a clean primary call, hiding the systemic event.
		logger.Warn("osm.mirror_failed",
			slog.String("endpoint", ep),
			slog.Int("attempt", i+1),
			slog.Int("total", len(endpoints)),
			slog.Any("err", err),
		)
		lastErr = err
	}
	// All mirrors down — escalate so the operator sees a single ERROR
	// instead of N anonymous warns. The error itself is still propagated
	// upstream so the caller (catalog refresh) aborts.
	logger.Error("osm.all_mirrors_failed",
		slog.Int("mirrors_tried", len(endpoints)),
		slog.Any("last_err", lastErr),
	)
	return nil, lastErr
}

// queryOne performs a single POST against `endpoint` and returns the body
// on success or an error (including non-2xx responses).
func (f *HTTPOverpassFetcher) queryOne(ctx context.Context, endpoint, ql string) ([]byte, error) {
	form := url.Values{}
	form.Set("data", ql)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("osm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// Some Overpass mirrors reject requests that do not carry a
	// recognisable User-Agent (Go-http-client/* may be blocked).
	// We must set this header ourselves because HTTPClient().Do() bypasses
	// httpx.Client.applyDefaultHeaders — that helper is only wired up
	// through GetBytes/GetJSON.
	req.Header.Set("User-Agent", httpx.DefaultUserAgent)
	resp, err := f.http.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("osm: overpass POST: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("osm: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Overpass returns plain-text error messages with rich details
		// (rate-limit hit, syntax error, server overloaded). We pass the
		// first 512 chars through so the caller can act / log.
		preview := string(body)
		if len(preview) > 512 {
			preview = preview[:512]
		}
		return nil, fmt.Errorf("osm: overpass HTTP %d: %s", resp.StatusCode, preview)
	}
	return body, nil
}

// RefreshCatalogFromOverpass runs one full France-wide query through
// `fetcher`, parses the response, and returns a freshly minted
// Catalog. Does NOT touch disk — caller is responsible for SaveCatalog.
// `bbox` may be empty (falls back to FranceMetropolitanBBox).
//
// Empty-result guard: a France-wide query that parses to zero stations
// is treated as a silent-mirror failure (common Overpass overload
// signature: HTTP 200 with `{"elements":[]}` instead of 429/504) and
// returns an error WITHOUT a catalog, matching the per-dept refresher's
// MinExpectedStations behaviour. The threshold check below the floor
// also prevents an obviously degraded snapshot from being persisted by
// a caller that forgot to validate the result.
//
// Deprecated: use RefreshCatalogFromOverpassByDepts which avoids
// server timeouts by issuing one small sub-query per department.
func RefreshCatalogFromOverpass(ctx context.Context, fetcher OverpassFetcher, bbox string) (*Catalog, error) {
	if fetcher == nil {
		return nil, errors.New("osm: nil fetcher")
	}
	if bbox == "" {
		bbox = FranceMetropolitanBBox
	}
	ql := FranceTransitOverpassQL(bbox)
	body, err := fetcher.Query(ctx, ql)
	if err != nil {
		return nil, err
	}
	stations, err := ParseOverpass(body)
	if err != nil {
		return nil, err
	}
	// Silent-mirror guard: a France-wide query MUST return non-empty.
	// An empty parse means the mirror served `{"elements":[]}` (the
	// classic overload-without-error signature) — surface as an error
	// instead of producing a successful but empty catalog.
	if len(stations) == 0 {
		return nil, fmt.Errorf("osm: France-wide query returned 0 stations (silent mirror overload, body=%d bytes)", len(body))
	}
	// Below-threshold guard mirrors RefreshCatalogFromOverpassByDepts so
	// the deprecated entry point cannot stealthily ship a degraded catalog.
	if len(stations) < MinExpectedStations {
		return nil, fmt.Errorf("osm: France-wide query below threshold (got=%d min=%d)", len(stations), MinExpectedStations)
	}
	// Companion query: routes + stop_areas so Station.Lines gets
	// populated. Failures here are tolerated — we'd rather ship a
	// catalog with empty `lines` than nothing.
	if routesBody, rerr := fetcher.Query(ctx, FranceTransitRoutesOverpassQL(bbox)); rerr == nil {
		if routes, stopAreas, perr := ParseOverpassRoutes(routesBody); perr == nil {
			AttachLinesFromRoutes(stations, routes, stopAreas)
		}
	}
	return &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		FetchedAt:     time.Now().UTC(),
		BBox:          bbox,
		Stations:      stations,
	}, nil
}

// RefreshCatalogFromOverpassByDepts fetches the transit station catalog
// by issuing one Overpass sub-query per metropolitan department (96 in
// total) instead of a single France-wide query.
//
// Motivation: the France-wide bbox query exceeds the server-side budget
// of every public Overpass mirror, resulting in timeouts or 406 errors.
// Individual department bboxes each cover at most a few hundred stations
// and complete in < 5 s, making the total refresh time ~3-5 minutes
// instead of "never".
//
// Failure policy:
//
//   - A per-dept network / parse failure is logged at warn level and the
//     loop continues with the remaining departments (partial result still
//     beats no result for an isolated mirror hiccup).
//   - A per-dept HTTP 200 with zero parsed stations is logged at warn
//     level via `osm.mirror_returned_empty`. The mirror sometimes returns
//     `[]` silently instead of an error when overloaded — without this
//     guard the refresh would happily declare success while writing an
//     empty catalog to disk.
//   - After the loop, the merged station count is validated against
//     MinExpectedStations. Below the floor the function returns an
//     error WITHOUT producing a catalog, so the caller (which gates
//     SaveCatalog on a non-nil result) never overwrites a healthy
//     on-disk snapshot with a degraded one.
//
// Deduplication: the same OSM node can appear in adjacent-department
// bboxes (padded by ~0.15° at the borders). Stations are deduplicated
// by (OSMType, OSMID) before the catalog is assembled.
//
// logger may be nil (falls back to slog.Default).
func RefreshCatalogFromOverpassByDepts(ctx context.Context, fetcher OverpassFetcher, logger *slog.Logger) (*Catalog, error) {
	if fetcher == nil {
		return nil, errors.New("osm: nil fetcher")
	}
	if logger == nil {
		logger = slog.Default()
	}

	type stationKey struct {
		osmType string
		osmID   int64
	}
	seen := make(map[stationKey]struct{}, 6000)
	merged := make([]Station, 0, 6000)
	failed := 0
	emptyResp := 0

	for _, dept := range FranceDepartmentBBoxes {
		if ctx.Err() != nil {
			break
		}
		ql := FranceTransitOverpassQL(dept.BBox)
		// Per-dept deadline: well above the typical 2-5 s for a healthy
		// Overpass response, but below the 60 s httpx retry wait that the
		// HTTP client would otherwise spend on a hung mirror. On failure
		// we warn-and-continue (partial result beats no result).
		deptCtx, deptCancel := context.WithTimeout(ctx, OverpassDeptTimeout)
		body, err := fetcher.Query(deptCtx, ql)
		deptCancel()
		if err != nil {
			logger.Warn("osm.dept_query_failed",
				slog.String("dept", dept.Code),
				slog.Any("err", err),
			)
			failed++
			continue
		}
		stations, err := ParseOverpass(body)
		if err != nil {
			logger.Warn("osm.dept_parse_failed",
				slog.String("dept", dept.Code),
				slog.Any("err", err),
			)
			failed++
			continue
		}
		if len(stations) == 0 {
			// 200 OK with `[]` — symptom of an overloaded mirror that
			// silently serves an empty response instead of returning a
			// 429 / 504. Track separately from `failed` so the
			// post-loop threshold check can distinguish "mirror dead"
			// from "many depts returned nothing".
			logger.Warn("osm.mirror_returned_empty",
				slog.String("dept", dept.Code),
				slog.Int("body_bytes", len(body)),
			)
			emptyResp++
			continue
		}
		// Companion query: routes + stop_areas so we can populate
		// Station.Lines. Failures here are warned-and-continued — we
		// still want the stations even when the lines lookup hiccups
		// (better "Lourmel" than nothing).
		var routes []Route
		var stopAreas []StopArea
		routesQL := FranceTransitRoutesOverpassQL(dept.BBox)
		routesCtx, routesCancel := context.WithTimeout(ctx, OverpassDeptTimeout)
		routesBody, rerr := fetcher.Query(routesCtx, routesQL)
		routesCancel()
		if rerr != nil {
			logger.Warn("osm.dept_routes_query_failed",
				slog.String("dept", dept.Code),
				slog.Any("err", rerr),
			)
		} else {
			routes, stopAreas, rerr = ParseOverpassRoutes(routesBody)
			if rerr != nil {
				logger.Warn("osm.dept_routes_parse_failed",
					slog.String("dept", dept.Code),
					slog.Any("err", rerr),
				)
			}
		}
		// Attach lines BEFORE dedup so a station that surfaces in two
		// neighbouring depts ends up with a unified lines list (the
		// second observation is dropped by dedup, but the first one
		// is now line-aware).
		if len(routes) > 0 || len(stopAreas) > 0 {
			AttachLinesFromRoutes(stations, routes, stopAreas)
		}

		added := 0
		linesPopulated := 0
		for _, st := range stations {
			k := stationKey{osmType: st.OSMType, osmID: st.OSMID}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, st)
			added++
			if len(st.Lines) > 0 {
				linesPopulated++
			}
		}
		logger.Info("osm.dept_done",
			slog.String("dept", dept.Code),
			slog.Int("stations_new", added),
			slog.Int("lines_populated", linesPopulated),
			slog.Int("routes_seen", len(routes)),
			slog.Int("stop_areas_seen", len(stopAreas)),
			slog.Int("total_so_far", len(merged)),
		)
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("osm: no stations fetched (failed=%d empty=%d)", failed, emptyResp)
	}
	if len(merged) < MinExpectedStations {
		// The refresh ran without a hard transport error but produced
		// far fewer stations than a healthy France-wide query yields.
		// Return an error so the caller does NOT call SaveCatalog —
		// keeping the previous on-disk snapshot intact.
		return nil, fmt.Errorf("osm: refresh below threshold (got=%d min=%d failed=%d empty=%d)",
			len(merged), MinExpectedStations, failed, emptyResp)
	}

	return &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		FetchedAt:     time.Now().UTC(),
		BBox:          FranceMetropolitanBBox,
		Stations:      merged,
	}, nil
}

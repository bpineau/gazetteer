package banx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// ErrIncoherentBANResponse is returned by validateCoherence when the
// BAN response has both CityCode and PostCode populated but their
// departement prefixes (first 2 chars) disagree outside of the legit
// Corsica / DOM-TOM cases. Used as a write-time guard in CachedGeocoder
// so cross-department drift never enters the persistent cache.
var ErrIncoherentBANResponse = errors.New("banx: incoherent BAN response (CityCode/PostCode dept prefix mismatch)")

// ErrDepartmentMismatch is returned by CachedGeocoder.Geocode when the
// caller provided an explicit input zip on the query but BAN returned a
// candidate whose PostCode is in a different département. Classic
// homonyme drift: querying "Bazainville 78550" returns a Loire commune
// in dept 42 because BAN's free-form matcher prefers a higher-scored
// homonyme when the address tokens are ambiguous. Reusing the same
// dept-guard semantics as the castorus / bienici / meilleursagents
// enrichers (memory `zipmatch_enricher_protocol`) — symmetric coverage
// across every fuzzy-resolver hop, no asymmetric leak.
//
// On this error the caller MUST treat the lookup as a miss; the cache
// is skipped so a transient drift does not poison persistent storage.
var ErrDepartmentMismatch = errors.New("banx: BAN returned a candidate outside the input zip's département")

// validateCoherence checks that the departement prefix of CityCode
// (INSEE) and PostCode agree. Returns ErrIncoherentBANResponse on
// disagreement. Exceptions:
//   - Corsica: INSEE prefix in {"2A","2B"} ↔ PostCode prefix == "20".
//   - DOM-TOM: both prefixes in {"97","98"} → coherent (any 97x/98x
//     combo, the 3rd digit identifies the territory and may legitimately
//     drift between INSEE and postal numbering).
//   - Either field empty → no validation; we only guard data we have.
func validateCoherence(res GeocodeResult) error {
	if len(res.CityCode) < 2 || len(res.PostCode) < 2 {
		return nil
	}
	cc := res.CityCode[:2]
	pc := res.PostCode[:2]
	if cc == pc {
		return nil
	}
	// Corsica: INSEE 2A/2B ↔ postal 20.
	if (cc == "2A" || cc == "2B") && pc == "20" {
		return nil
	}
	// DOM-TOM: both prefixes are 97 or 98 (any combo).
	if (cc == "97" || cc == "98") && (pc == "97" || pc == "98") {
		return nil
	}
	return ErrIncoherentBANResponse
}

// CachedGeocoder wraps a delegate Geocoder with a persistent cache
// behind a kvcache.Cache. Same-query → 1 underlying call. The cache TTL
// is configurable; the default (1 year) reflects the fact that street
// addresses rarely move.
//
// The cache backend is opaque to this type; consumers wire either the
// in-memory kvcache/memcache (tests) or store.NewKVCacheAdapter (prod,
// via the bun-backed kv_cache table).
//
// Not safe for concurrent use from multiple goroutines: callers must
// ensure that Geocode / Reverse are not called concurrently on the same
// instance unless the underlying kvcache.Cache and delegate are both
// goroutine-safe (which every production implementation is).
type CachedGeocoder struct {
	delegate Geocoder
	cache    kvcache.Cache
	ttl      time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewCachedGeocoder wraps delegate with a kvcache.Cache-backed cache.
// ttl=0 → 1-year fallback. The logger defaults to slog.Default() and is
// used only for rare write-skip warnings (incoherent BAN response). Use
// WithLogger to override it.
func NewCachedGeocoder(delegate Geocoder, c kvcache.Cache, ttl time.Duration) *CachedGeocoder {
	if ttl == 0 {
		ttl = 365 * 24 * time.Hour
	}
	return &CachedGeocoder{
		delegate: delegate,
		cache:    c,
		ttl:      ttl,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   slog.Default(),
	}
}

// WithLogger attaches a *slog.Logger to the geocoder and returns the
// receiver, enabling method chaining at construction time:
//
//	cached := banx.NewCachedGeocoder(ban, store, 0).WithLogger(lg)
//
// A nil logger resets to slog.Default().
func (c *CachedGeocoder) WithLogger(lg *slog.Logger) *CachedGeocoder {
	if lg == nil {
		lg = slog.Default()
	}
	c.logger = lg
	return c
}

// Geocode implements Geocoder. Hits the cache first; falls back to the
// delegate on miss; persists the result on success.
func (c *CachedGeocoder) Geocode(ctx context.Context, q GeocodeQuery) (GeocodeResult, error) {
	key := CacheKey(q)
	if row, err := c.cache.Get(ctx, key); err == nil {
		// Honor TTL if expires_at set; otherwise cached forever.
		if row.ExpiresAt == nil || c.now().Before(*row.ExpiresAt) {
			var res GeocodeResult
			if jErr := json.Unmarshal(row.Value, &res); jErr == nil {
				// Read-side dept-coherence guard. Defense-in-depth:
				// even though the write-side guard prevents new
				// poisoned rows, legacy entries written before the
				// guard existed (or under a different code path that
				// fed a wrong zip — e.g. a lawyer-address leak) may
				// still be sitting in the persistent cache. A cached
				// hit whose PostCode disagrees with the caller's
				// input zip département is treated as a MISS so the
				// delegate gets a fresh shot at the right commune.
				// Mirrors the picker-time dept-guard semantics shared
				// across the castorus / bienici / meilleursagents
				// fuzzy-resolver cohort.
				if inputZip := strings.TrimSpace(q.Zip); inputZip != "" && res.PostCode != "" &&
					!ZipsShareDepartment(inputZip, res.PostCode) {
					c.logger.Warn("geocode.cache.dept_mismatch_miss: cached PostCode outside input zip département",
						slog.String("query", q.String()),
						slog.String("input_zip", inputZip),
						slog.String("cached_postcode", res.PostCode),
						slog.String("cached_label", res.Label),
					)
					// fallthrough to delegate (treat as MISS)
				} else {
					return res, nil
				}
			}
			// fallthrough on json error → re-query
		}
	} else if !errors.Is(err, kvcache.ErrNotFound) {
		return GeocodeResult{}, fmt.Errorf("banx cache: %w", err)
	}

	res, err := c.delegate.Geocode(ctx, q)
	if err != nil {
		return GeocodeResult{}, err
	}
	if res.FetchedAt.IsZero() {
		res.FetchedAt = c.now()
	}
	// Input-vs-output dept-guard: when the caller anchored the query
	// with an explicit zip and BAN returned a candidate in a different
	// département, the result is a homonyme drift (e.g. Bazainville
	// 78550 → a Loire commune in dept 42). Reject upstream — the caller
	// sees ErrDepartmentMismatch and treats it as a miss; the cache is
	// not poisoned. Mirrors the dept-guard in MA / castorus / bienici
	// pickers (memory `zipmatch_enricher_protocol`).
	if inputZip := strings.TrimSpace(q.Zip); inputZip != "" && res.PostCode != "" {
		if !ZipsShareDepartment(inputZip, res.PostCode) {
			c.logger.Warn("geocode.dept_mismatch: BAN candidate outside input zip département",
				slog.String("query", q.String()),
				slog.String("input_zip", inputZip),
				slog.String("ban_postcode", res.PostCode),
				slog.String("ban_label", res.Label),
			)
			return GeocodeResult{}, ErrDepartmentMismatch
		}
	}
	// Write-time guard: refuse to persist incoherent BAN responses
	// (CityCode dept prefix disagrees with PostCode dept prefix).
	// The caller still gets the result so a single transient drift
	// doesn't break consumers; we just don't poison the cache with it.
	if err := validateCoherence(res); err != nil {
		c.logger.Warn("geocode.cache.write_skipped: incoherent BAN response",
			slog.String("query", q.String()),
			slog.String("citycode", res.CityCode),
			slog.String("postcode", res.PostCode),
		)
		return res, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return res, nil // serve from delegate even if cache write fails
	}
	exp := c.now().Add(c.ttl)
	_ = c.cache.Set(ctx, kvcache.Entry{
		Key:       key,
		Value:     b,
		FetchedAt: c.now(),
		ExpiresAt: &exp,
	})
	return res, nil
}

// Reverse implements ReverseGeocoder when the underlying delegate also
// supports reverse-geocoding (in practice: *BANClient). Mirrors the
// forward-cache path with a separate cache-key namespace
// ("geocode:ban_reverse:") keyed by truncated lat/lon.
//
// This wiring is required for INSEEResolver to reach the BAN reverse
// step when the resolver only sees a *CachedGeocoder handle: without
// it, low-score forwards would bubble up as ErrNotFound even when the
// input lat/lon are populated.
//
// When the delegate does not implement ReverseGeocoder, returns an
// explicit error. Callers that probe via _, ok := g.(ReverseGeocoder)
// will still see the assertion succeed (Go interfaces are structural),
// so check the returned error before trusting the result.
func (c *CachedGeocoder) Reverse(ctx context.Context, lat, lon float64) (GeocodeResult, error) {
	rev, ok := c.delegate.(ReverseGeocoder)
	if !ok {
		return GeocodeResult{}, fmt.Errorf("banx: delegate does not implement ReverseGeocoder")
	}
	key := ReverseCacheKey(lat, lon)
	if row, err := c.cache.Get(ctx, key); err == nil {
		if row.ExpiresAt == nil || c.now().Before(*row.ExpiresAt) {
			var res GeocodeResult
			if jErr := json.Unmarshal(row.Value, &res); jErr == nil {
				return res, nil
			}
		}
	} else if !errors.Is(err, kvcache.ErrNotFound) {
		return GeocodeResult{}, fmt.Errorf("banx reverse cache: %w", err)
	}

	res, err := rev.Reverse(ctx, lat, lon)
	if err != nil {
		return GeocodeResult{}, err
	}
	if res.FetchedAt.IsZero() {
		res.FetchedAt = c.now()
	}
	// Same write-time guard as the forward path — keeps cross-department
	// drift from sneaking in via the reverse cache namespace.
	if err := validateCoherence(res); err != nil {
		c.logger.Warn("geocode.cache.write_skipped: incoherent BAN response",
			slog.Float64("lat", lat),
			slog.Float64("lon", lon),
			slog.String("citycode", res.CityCode),
			slog.String("postcode", res.PostCode),
		)
		return res, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return res, nil
	}
	exp := c.now().Add(c.ttl)
	_ = c.cache.Set(ctx, kvcache.Entry{
		Key:       key,
		Value:     b,
		FetchedAt: c.now(),
		ExpiresAt: &exp,
	})
	return res, nil
}

// Delegate exposes the wrapped Geocoder so callers can recover the
// concrete underlying client when they need a capability the decorator
// does not expose. New code should prefer the ReverseGeocoder interface
// assertion on the wrapper itself.
func (c *CachedGeocoder) Delegate() Geocoder { return c.delegate }

// CacheKey returns the kv_cache key for a query. Stable across calls /
// case-insensitive on the address string.
func CacheKey(q GeocodeQuery) string {
	canon := strings.ToLower(strings.TrimSpace(q.String()))
	h := sha256.Sum256([]byte(canon))
	return "geocode:ban:" + hex.EncodeToString(h[:16])
}

// ReverseCacheKey returns the kv_cache key for a (lat, lon) lookup.
// Coordinates are rounded to 6 decimals (~11cm) to keep the namespace
// bounded; BAN reverse precision is well above this granularity.
func ReverseCacheKey(lat, lon float64) string {
	canon := fmt.Sprintf("%.6f,%.6f", lat, lon)
	h := sha256.Sum256([]byte(canon))
	return "geocode:ban_reverse:" + hex.EncodeToString(h[:16])
}

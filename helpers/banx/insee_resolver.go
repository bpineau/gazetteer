package banx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// INSEEResolver resolves an input (free-form address and/or lat/lon)
// to a 5-digit INSEE commune code via the following cascade:
//
//  1. BAN forward on the free-form address. If score >= MinForwardScore
//     and citycode != "" → use it. Source = "ban_forward".
//  2. BAN reverse if the input carries lat/lon. The pin position is by
//     construction in the correct commune, independent of address-text
//     matching. Source = "ban_reverse".
//  3. Failure with ErrNotFound. The cascade deliberately does not fall
//     back to a zip→INSEE table because Paris/Lyon/Marseille have
//     multiple INSEE per zip — guessing wrong is worse than not knowing.
//
// The MinForwardScore default (0.7) is calibrated to filter BAN's
// "best-effort" matches on partial addresses. For an exact street+city
// match, BAN typically returns 0.85–0.99.
type INSEEResolver struct {
	// Forward is the BAN forward geocoder (free-form address → coord).
	// Required.
	Forward Geocoder
	// Reverse is the BAN reverse geocoder (coord → INSEE). When nil,
	// the resolver skips step 2 and goes straight to ErrNotFound.
	Reverse ReverseGeocoder
	// MinForwardScore is the threshold above which a BAN forward result
	// is trusted. Defaults to 0.7 when zero.
	MinForwardScore float64
}

// ReverseGeocoder is the contract for coord → INSEE lookup. Mirrors
// `Geocoder` but indexed by lat/lon. The BANClient implements both.
type ReverseGeocoder interface {
	Reverse(ctx context.Context, lat, lon float64) (GeocodeResult, error)
}

// Unwrapper exposes the wrapped Geocoder underneath a decorator (e.g.
// CachedGeocoder). Used by callers that need the *concrete* underlying
// client — typically because they require capabilities the decorator
// does not expose (BANClient implements ReverseGeocoder; the cache
// wrapper does not). Any decorator that should be transparent to such
// callers must implement Unwrapper.
type Unwrapper interface {
	Delegate() Geocoder
}

// INSEEQuery is the input to Resolve. At least one of Address+City+Zip
// (for forward) OR (Lat+Lon non-zero, for reverse) must be set.
type INSEEQuery struct {
	Address string
	City    string
	Zip     string
	// Lat/Lon are optional structured coordinates. When both non-zero,
	// the reverse fallback is enabled.
	Lat float64
	Lon float64
}

// INSEEResolution is what Resolve returns on success. Source identifies
// which step of the cascade resolved it (`ban_forward` or `ban_reverse`)
// for traceability in enricher payloads.
type INSEEResolution struct {
	INSEE  string
	Lat    float64
	Lon    float64
	Source string // "ban_forward" | "ban_reverse"
}

// Resolve runs the cascade. Returns ErrNotFound when no step yields an
// INSEE.
func (r *INSEEResolver) Resolve(ctx context.Context, q INSEEQuery) (INSEEResolution, error) {
	if r == nil || r.Forward == nil {
		return INSEEResolution{}, errors.New("INSEEResolver: nil Forward geocoder")
	}
	hasText := strings.TrimSpace(q.Address) != "" ||
		strings.TrimSpace(q.City) != "" ||
		strings.TrimSpace(q.Zip) != ""
	hasCoords := q.Lat != 0 && q.Lon != 0

	// Step 1: BAN forward on the free-form address.
	if hasText {
		fwd, err := r.Forward.Geocode(ctx, GeocodeQuery{
			Address: q.Address, City: q.City, Zip: q.Zip,
		})
		if err == nil {
			minScore := r.MinForwardScore
			if minScore == 0 {
				minScore = 0.7
			}
			// Trust forward when citycode non-empty AND (Score absent OR
			// Score ≥ threshold). Score=0 is treated as "unknown, trust
			// the result" rather than "low confidence", so test mocks
			// and proxies that omit Score don't force a fallback.
			// Real BAN always reports Score in (0, 1].
			if fwd.CityCode != "" && (fwd.Score == 0 || fwd.Score >= minScore) {
				return INSEEResolution{
					INSEE:  fwd.CityCode,
					Lat:    fwd.Lat,
					Lon:    fwd.Lon,
					Source: "ban_forward",
				}, nil
			}
			// Forward returned but below threshold → fallback if coords.
		} else if !errors.Is(err, ErrNotFound) {
			// Hard error (HTTP / decode) — try the reverse fallback if
			// possible, else propagate.
			if !hasCoords || r.Reverse == nil {
				return INSEEResolution{}, fmt.Errorf("INSEEResolver: forward: %w", err)
			}
		}
	}

	// Step 2: BAN reverse on the coords.
	if hasCoords && r.Reverse != nil {
		rev, err := r.Reverse.Reverse(ctx, q.Lat, q.Lon)
		if err == nil && rev.CityCode != "" {
			return INSEEResolution{
				INSEE:  rev.CityCode,
				Lat:    q.Lat, // keep the input coords; they are by definition correct
				Lon:    q.Lon,
				Source: "ban_reverse",
			}, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return INSEEResolution{}, fmt.Errorf("INSEEResolver: reverse: %w", err)
		}
	}

	return INSEEResolution{}, ErrNotFound
}

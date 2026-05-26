package banx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/pkg/httpx"
)

// BANEndpoint is the base URL of the BAN search API. Exposed as a var so
// tests can swap it.
var BANEndpoint = "https://api-adresse.data.gouv.fr/search/"

// BANReverseEndpoint is the base URL of the BAN reverse-geocoding API.
// Returns the same GeoJSON shape as `/search/` but indexed by lat/lon
// instead of free-form address. More reliable than forward-search when
// the upstream payload exposes coords directly.
var BANReverseEndpoint = "https://api-adresse.data.gouv.fr/reverse/"

// BANClient is the HTTP-backed Geocoder hitting api-adresse.data.gouv.fr.
type BANClient struct {
	http *httpx.Client
}

// NewBANClient builds a BANClient on top of the shared httpx client.
func NewBANClient(http *httpx.Client) *BANClient {
	return &BANClient{http: http}
}

// banFeatureCollection is the GeoJSON envelope returned by BAN.
type banFeatureCollection struct {
	Features []banFeature `json:"features"`
}

type banFeature struct {
	Geometry   banGeometry   `json:"geometry"`
	Properties banProperties `json:"properties"`
}

type banGeometry struct {
	Coordinates []float64 `json:"coordinates"` // [lon, lat]
}

type banProperties struct {
	Label    string  `json:"label"`
	Score    float64 `json:"score"`
	CityCode string  `json:"citycode"`
	PostCode string  `json:"postcode"`
}

// banMaxQueryLen caps the `q` parameter at BAN's documented limit of
// 200 chars. Upstream code occasionally passes much longer strings
// (descriptions, paragraphs, …) as the address, producing URLs that
// BAN rejects with HTTP 400. We truncate defensively here so a single
// bad address can't cascade into a stream of failed lookups.
const banMaxQueryLen = 200

// Geocode implements Geocoder. Returns ErrNotFound when BAN returns an
// empty feature collection or when the query is bad enough to be
// equivalent to no input (over the API's 200-char cap).
func (c *BANClient) Geocode(ctx context.Context, q GeocodeQuery) (GeocodeResult, error) {
	if c == nil || c.http == nil {
		return GeocodeResult{}, errors.New("banx: nil http client")
	}
	query := strings.TrimSpace(q.String())
	if query == "" {
		return GeocodeResult{}, errors.New("banx: empty query")
	}
	if len(query) > banMaxQueryLen {
		query = strings.TrimSpace(query[:banMaxQueryLen])
	}
	u, err := url.Parse(BANEndpoint)
	if err != nil {
		return GeocodeResult{}, fmt.Errorf("banx: parse endpoint: %w", err)
	}
	v := u.Query()
	v.Set("q", query)
	v.Set("limit", "1")
	u.RawQuery = v.Encode()

	var fc banFeatureCollection
	if err := c.http.GetJSON(ctx, u.String(), nil, &fc); err != nil {
		return GeocodeResult{}, fmt.Errorf("banx: BAN GET: %w", err)
	}
	if len(fc.Features) == 0 {
		return GeocodeResult{}, ErrNotFound
	}
	f := fc.Features[0]
	if len(f.Geometry.Coordinates) < 2 {
		return GeocodeResult{}, fmt.Errorf("banx: BAN forward returned malformed coordinates for q=%q (got %d coords, want >=2)", query, len(f.Geometry.Coordinates))
	}
	return GeocodeResult{
		Lat:       f.Geometry.Coordinates[1],
		Lon:       f.Geometry.Coordinates[0],
		Label:     f.Properties.Label,
		Score:     f.Properties.Score,
		CityCode:  f.Properties.CityCode,
		PostCode:  f.Properties.PostCode,
		Source:    "ban",
		FetchedAt: time.Now().UTC(),
	}, nil
}

// Reverse resolves (lat, lon) → INSEE/postcode/label via the BAN
// reverse API. Useful when the upstream source already exposes precise
// coordinates and the caller wants a fully reliable INSEE without
// exposing itself to the fragility of the free-form address text
// matcher.
//
// Returns ErrNotFound when BAN returns no feature for the input.
func (c *BANClient) Reverse(ctx context.Context, lat, lon float64) (GeocodeResult, error) {
	if c == nil || c.http == nil {
		return GeocodeResult{}, errors.New("banx: nil http client")
	}
	u, err := url.Parse(BANReverseEndpoint)
	if err != nil {
		return GeocodeResult{}, fmt.Errorf("banx: parse reverse endpoint: %w", err)
	}
	v := u.Query()
	v.Set("lat", fmt.Sprintf("%.7f", lat))
	v.Set("lon", fmt.Sprintf("%.7f", lon))
	v.Set("limit", "1")
	u.RawQuery = v.Encode()

	var fc banFeatureCollection
	if err := c.http.GetJSON(ctx, u.String(), nil, &fc); err != nil {
		return GeocodeResult{}, fmt.Errorf("banx: BAN reverse GET: %w", err)
	}
	if len(fc.Features) == 0 {
		return GeocodeResult{}, ErrNotFound
	}
	f := fc.Features[0]
	if len(f.Geometry.Coordinates) < 2 {
		return GeocodeResult{}, fmt.Errorf("banx: BAN reverse returned malformed coordinates for (lat=%.7f, lon=%.7f) (got %d coords, want >=2)", lat, lon, len(f.Geometry.Coordinates))
	}
	return GeocodeResult{
		Lat:       f.Geometry.Coordinates[1],
		Lon:       f.Geometry.Coordinates[0],
		Label:     f.Properties.Label,
		Score:     f.Properties.Score,
		CityCode:  f.Properties.CityCode,
		PostCode:  f.Properties.PostCode,
		Source:    "ban_reverse",
		FetchedAt: time.Now().UTC(),
	}, nil
}

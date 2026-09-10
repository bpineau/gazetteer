package osm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

// gzipped returns body gzip-compressed, the on-disk shape of the processed
// artifact.
func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseCatalog(t *testing.T) {
	t.Parallel()
	good := gzipped(t, []byte(`{"schema_version":2,"bbox":"france","stations":[{"osm_type":"node","osm_id":1,"name":"Lourmel","type":"metro"}]}`))

	cases := []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{"plain JSON, not gzipped", []byte(`{"stations":[]}`), "gunzip"},
		{"truncated gzip stream", gzipped(t, []byte(`{"stations":[]}`))[:8], "gunzip"},
		{"gzip of garbage", gzipped(t, []byte("{not json")), "parse catalog"},
		{"good", good, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cat, err := parseCatalog(bytes.NewReader(c.body))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("parseCatalog = %v, want an error containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCatalog: %v", err)
			}
			if len(cat.Stations) != 1 || cat.Stations[0].Name != "Lourmel" {
				t.Errorf("catalog = %+v", cat)
			}
		})
	}
}

func TestValidateCatalog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		body    []byte
		wantErr string
	}{
		{"unreadable artifact", []byte("not gzip"), "gunzip"},
		{"no stations", gzipped(t, []byte(`{"schema_version":2,"stations":[]}`)), "no stations"},
		{"missing stations key", gzipped(t, []byte(`{"schema_version":2}`)), "no stations"},
		{
			name: "populated",
			body: gzipped(t, []byte(`{"schema_version":2,"stations":[{"osm_id":1,"name":"x","type":"metro"}]}`)),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := validate(bytes.NewReader(c.body))
			switch {
			case c.wantErr == "":
				if err != nil {
					t.Errorf("validate = %v, want nil", err)
				}
			case err == nil || !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("validate = %v, want an error containing %q", err, c.wantErr)
			}
		})
	}
}

// TestTransform_NeedsAnHTTPClient: unlike the file-download Sources, this
// one's refresh rebuilds the catalog from a live Overpass walk, so the
// client dataset.Refresh injects into the context is mandatory.
func TestTransform_NeedsAnHTTPClient(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := transform(context.Background(), nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "HTTP client") {
		t.Errorf("transform without a client = %v, want an explicit refusal", err)
	}
}

// TestTransform_RebuildsFromOverpass drives the whole refresh leg against a
// fake mirror: the department walk, the merge, the gzip write, and the
// artifact the validator then accepts.
func TestTransform_RebuildsFromOverpass(t *testing.T) {
	stations := loadFixture(t, "paris15_sample.json")
	routes := loadFixture(t, "paris_routes_sample.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.Contains(r.Form.Get("data"), "stop_area") {
			_, _ = w.Write(routes)
			return
		}
		_, _ = w.Write(stations)
	}))
	defer srv.Close()

	// Point the mirror rotation at the fake server for the duration.
	oldEP, oldFB := OverpassEndpoint, OverpassFallbackEndpoints
	OverpassEndpoint, OverpassFallbackEndpoints = srv.URL, nil
	t.Cleanup(func() { OverpassEndpoint, OverpassFallbackEndpoints = oldEP, oldFB })
	setDepts(t, DeptBBox{"75", "48.81,2.22,48.91,2.42"})
	setMinStations(t, 1)

	hc, err := httpx.New(httpx.Options{RateLimitPerHost: 1000, BurstPerHost: 1000, MaxRetries: -1})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	ctx := dataset.WithHTTPClient(context.Background(), hc)

	var buf bytes.Buffer
	if err := transform(ctx, nil, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("the rebuilt artifact does not validate: %v", err)
	}
	cat, err := parseCatalog(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	want, err := ParseOverpass(stations)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Stations) != len(want) {
		t.Errorf("stations = %d, want %d", len(cat.Stations), len(want))
	}
	if cat.SchemaVersion != CatalogSchemaVersion || cat.BBox != FranceMetropolitanBBox {
		t.Errorf("envelope = v%d / %q", cat.SchemaVersion, cat.BBox)
	}
	if time.Since(cat.FetchedAt) > time.Minute {
		t.Errorf("FetchedAt = %v, want a fresh stamp", cat.FetchedAt)
	}
}

// TestTransform_PropagatesRefreshFailure: a refresh that cannot reach its
// floor must fail the transform, so dataset.Refresh keeps the previous
// artifact instead of publishing a degraded one.
func TestTransform_PropagatesRefreshFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	oldEP, oldFB := OverpassEndpoint, OverpassFallbackEndpoints
	OverpassEndpoint, OverpassFallbackEndpoints = srv.URL, nil
	t.Cleanup(func() { OverpassEndpoint, OverpassFallbackEndpoints = oldEP, oldFB })
	setDepts(t, DeptBBox{"75", "48.81,2.22,48.91,2.42"})
	setMinStations(t, 1)

	hc, err := httpx.New(httpx.Options{RateLimitPerHost: 1000, BurstPerHost: 1000, MaxRetries: -1})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	var buf bytes.Buffer
	err = transform(dataset.WithHTTPClient(context.Background(), hc), nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "overpass refresh") {
		t.Fatalf("transform = %v, want the refresh failure propagated", err)
	}
	if buf.Len() > 0 {
		t.Errorf("a failed transform wrote %d bytes, want none", buf.Len())
	}
}

// TestDatasets exposes the refresh wiring the CLI's `refresh` command reads.
func TestDatasets(t *testing.T) {
	t.Parallel()
	sets := NewSource(Options{Catalog: &Catalog{}}).Datasets()
	if len(sets) != 1 {
		t.Fatalf("Datasets = %d entries, want 1", len(sets))
	}
	s := sets[0]
	if s.Source != Name || s.Version != Version {
		t.Errorf("set = %q v%d, want %q v%d", s.Source, s.Version, Name, Version)
	}
	if s.Processed.Name == "" {
		t.Error("the processed artifact must be named")
	}
	if len(s.Raw) != 0 {
		t.Errorf("Raw = %+v, want none (the refresh has no static input)", s.Raw)
	}
	if s.Transform == nil || s.Validate == nil {
		t.Error("the set must carry both a Transform and a Validate")
	}
}

// TestEmbeddedCatalogIsUsable smokes the committed baseline: it decodes, and
// it covers metropolitan France densely enough to answer a central address.
func TestEmbeddedCatalogIsUsable(t *testing.T) {
	t.Parallel()
	rc, err := set.Open("")
	if err != nil {
		t.Fatalf("open embedded catalog: %v", err)
	}
	defer func() { _ = rc.Close() }()
	cat, err := parseCatalog(rc)
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	if len(cat.Stations) < MinExpectedStations {
		t.Errorf("embedded catalog carries %d stations, below the %d floor",
			len(cat.Stations), MinExpectedStations)
	}
	if cat.SchemaVersion != CatalogSchemaVersion {
		t.Errorf("embedded schema = %d, want %d", cat.SchemaVersion, CatalogSchemaVersion)
	}
	// The JSON shape the loader reads is the one SaveCatalog writes.
	raw, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseCatalog(bytes.NewReader(gzipped(t, raw))); err != nil {
		t.Errorf("round-trip through the on-disk shape: %v", err)
	}
	if _, err := io.ReadAll(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
}

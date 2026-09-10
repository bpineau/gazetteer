package osm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubOverpass answers QL queries from memory, so the refresher is exercised
// with no network at all. The two companion queries are told apart by their
// bodies: only the routes QL mentions stop_area.
type stubOverpass struct {
	mu       sync.Mutex
	stations func(ql string) ([]byte, error)
	routes   func(ql string) ([]byte, error)
	calls    int
}

func (s *stubOverpass) Query(ctx context.Context, ql string) ([]byte, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(ql, "stop_area") {
		if s.routes == nil {
			return []byte(`{"elements":[]}`), nil
		}
		return s.routes(ql)
	}
	if s.stations == nil {
		return []byte(`{"elements":[]}`), nil
	}
	return s.stations(ql)
}

func (s *stubOverpass) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// recorder is a slog.Handler that keeps every message it is given, so a test
// can assert on the refresher's observability contract (the warn/error lines
// an operator relies on to tell a partial outage from a clean run).
type recorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, rec.Message)
	return nil
}
func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func (r *recorder) has(msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

func newRecorder() (*slog.Logger, *recorder) {
	r := &recorder{}
	return slog.New(r), r
}

// setDepts shrinks the department table for the duration of a test: the
// refresher walks it in full, and a synthetic fixture has nothing to say
// about 96 bboxes.
func setDepts(t *testing.T, depts ...DeptBBox) {
	t.Helper()
	old := FranceDepartmentBBoxes
	FranceDepartmentBBoxes = depts
	t.Cleanup(func() { FranceDepartmentBBoxes = old })
}

// setMinStations lowers (or raises) the refresh acceptance floor.
func setMinStations(t *testing.T, n int) {
	t.Helper()
	old := MinExpectedStations
	MinExpectedStations = n
	t.Cleanup(func() { MinExpectedStations = old })
}

func TestRefreshCatalogFromOverpassByDepts_MergesAndDedups(t *testing.T) {
	stationsBody := loadFixture(t, "paris15_sample.json")
	routesBody := loadFixture(t, "paris_routes_sample.json")
	want, err := ParseOverpass(stationsBody)
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}

	// Two departments answering with the SAME stations: the (OSMType, OSMID)
	// dedup must collapse them, as it does for a station straddling a
	// padded department border.
	setDepts(t, DeptBBox{"75", "48.81,2.22,48.91,2.42"}, DeptBBox{"92", "48.78,2.15,48.96,2.35"})
	setMinStations(t, 1)

	stub := &stubOverpass{
		stations: func(string) ([]byte, error) { return stationsBody, nil },
		routes:   func(string) ([]byte, error) { return routesBody, nil },
	}
	logger, rec := newRecorder()

	cat, err := RefreshCatalogFromOverpassByDepts(context.Background(), stub, logger)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if cat.SchemaVersion != CatalogSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", cat.SchemaVersion, CatalogSchemaVersion)
	}
	if cat.BBox != FranceMetropolitanBBox {
		t.Errorf("BBox = %q, want the metropolitan box", cat.BBox)
	}
	if time.Since(cat.FetchedAt) > time.Minute || cat.FetchedAt.Location() != time.UTC {
		t.Errorf("FetchedAt = %v, want a fresh UTC stamp", cat.FetchedAt)
	}
	if len(cat.Stations) != len(want) {
		t.Errorf("stations = %d, want %d (the second department is all duplicates)", len(cat.Stations), len(want))
	}
	// One stations query + one routes query per department.
	if got := stub.callCount(); got != 4 {
		t.Errorf("fetcher calls = %d, want 4 (2 depts x 2 queries)", got)
	}
	if !rec.has("osm.dept_done") {
		t.Error("no osm.dept_done line: the per-department progress log is the refresh's only trace")
	}
}

func TestRefreshCatalogFromOverpassByDepts_Failures(t *testing.T) {
	stationsBody := loadFixture(t, "paris15_sample.json")

	cases := []struct {
		name     string
		stub     *stubOverpass
		minStat  int
		wantErr  string
		wantLogs []string
	}{
		{
			name: "every department fails",
			stub: &stubOverpass{
				stations: func(string) ([]byte, error) { return nil, errors.New("mirror down") },
			},
			minStat:  1,
			wantErr:  "no stations fetched (failed=2",
			wantLogs: []string{"osm.dept_query_failed"},
		},
		{
			name: "unparsable answers",
			stub: &stubOverpass{
				stations: func(string) ([]byte, error) { return []byte("{not json"), nil },
			},
			minStat:  1,
			wantErr:  "no stations fetched (failed=2",
			wantLogs: []string{"osm.dept_parse_failed"},
		},
		{
			name:     "silently empty answers",
			stub:     &stubOverpass{}, // 200 OK with `[]`
			minStat:  1,
			wantErr:  "empty=2",
			wantLogs: []string{"osm.mirror_returned_empty"},
		},
		{
			name: "below the acceptance floor",
			stub: &stubOverpass{
				stations: func(string) ([]byte, error) { return stationsBody, nil },
			},
			minStat: 100_000,
			wantErr: "below threshold",
		},
		{
			name: "routes leg fails, stations still ship",
			stub: &stubOverpass{
				stations: func(string) ([]byte, error) { return stationsBody, nil },
				routes:   func(string) ([]byte, error) { return nil, errors.New("routes mirror down") },
			},
			minStat:  1,
			wantLogs: []string{"osm.dept_routes_query_failed"},
		},
		{
			name: "routes leg unparsable, stations still ship",
			stub: &stubOverpass{
				stations: func(string) ([]byte, error) { return stationsBody, nil },
				routes:   func(string) ([]byte, error) { return []byte("{not json"), nil },
			},
			minStat:  1,
			wantLogs: []string{"osm.dept_routes_parse_failed"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setDepts(t, DeptBBox{"75", "48.81,2.22,48.91,2.42"}, DeptBBox{"92", "48.78,2.15,48.96,2.35"})
			setMinStations(t, c.minStat)
			logger, rec := newRecorder()

			cat, err := RefreshCatalogFromOverpassByDepts(context.Background(), c.stub, logger)
			switch c.wantErr {
			case "":
				if err != nil {
					t.Fatalf("refresh = %v, want nil (the stations leg succeeded)", err)
				}
				if cat == nil || len(cat.Stations) == 0 {
					t.Fatal("want a populated catalog despite the routes-leg failure")
				}
			default:
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("refresh = %v, want an error containing %q", err, c.wantErr)
				}
				// A refresh that failed its own checks must NOT hand back a
				// catalog: the caller gates SaveCatalog on a non-nil result
				// and would otherwise overwrite a healthy snapshot.
				if cat != nil {
					t.Errorf("failed refresh returned a catalog (%d stations), want nil", len(cat.Stations))
				}
			}
			for _, want := range c.wantLogs {
				if !rec.has(want) {
					t.Errorf("missing log line %q", want)
				}
			}
		})
	}
}

func TestRefreshCatalogFromOverpassByDepts_NilFetcher(t *testing.T) {
	if _, err := RefreshCatalogFromOverpassByDepts(context.Background(), nil, nil); err == nil {
		t.Error("a nil fetcher must be refused")
	}
}

// TestRefreshCatalogFromOverpassByDepts_CancelledContext: the loop checks the
// context before each department, so an abandoned refresh stops immediately
// instead of walking 96 bboxes.
func TestRefreshCatalogFromOverpassByDepts_CancelledContext(t *testing.T) {
	setDepts(t, DeptBBox{"75", "48.81,2.22,48.91,2.42"}, DeptBBox{"92", "48.78,2.15,48.96,2.35"})
	setMinStations(t, 1)
	stub := &stubOverpass{
		stations: func(string) ([]byte, error) { return loadFixture(t, "paris15_sample.json"), nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cat, err := RefreshCatalogFromOverpassByDepts(ctx, stub, nil)
	if err == nil || !strings.Contains(err.Error(), "no stations fetched") {
		t.Fatalf("refresh under a cancelled context = (%v, %v), want a no-stations error", cat, err)
	}
	if stub.callCount() != 0 {
		t.Errorf("fetcher called %d times under a cancelled context, want 0", stub.callCount())
	}
}

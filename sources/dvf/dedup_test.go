package dvf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// stubCommunes lets a test pin the exact neighborhood / department
// fan-out lists, mirroring the real table's superset semantics
// (Neighbors INCLUDES the primary; SameDepartment includes primary +
// neighbours) without dragging ~35 000 real communes into the ladder.
type stubCommunes struct {
	neighbors  []string
	department []string
}

func (c stubCommunes) Lookup(string) (communes.Commune, bool) { return communes.Commune{}, false }
func (c stubCommunes) Neighbors(string, float64) []string     { return c.neighbors }
func (c stubCommunes) SameDepartment(string) []string         { return c.department }

// hitCounter records per-path request counts for an httptest server.
type hitCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

func newHitCounter() *hitCounter { return &hitCounter{hits: make(map[string]int)} }

func (h *hitCounter) inc(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hits[path]++
}

func (h *hitCounter) get(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hits[path]
}

func (h *hitCounter) total() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.hits {
		n += c
	}
	return n
}

// validMutationsBody returns a DVF API envelope with n filter-surviving
// apartment sales (distinct parcelles so the per-parcelle cap never
// kicks in). When lat/lon are non-nil the rows carry coordinates inside
// the address_radius disk around that point.
func validMutationsBody(t *testing.T, n int, idPrefix string, lat, lon *float64) []byte {
	t.Helper()
	resp := MutationsResponse{}
	for i := 0; i < n; i++ {
		v := 250_000.0
		s := 50.0
		m := Mutation{
			IDMutation:        fmt.Sprintf("%s-%d", idPrefix, i),
			DateMutation:      "2025-06-01",
			NatureMutation:    NatureMutationVente,
			ValeurFonciere:    &v,
			TypeLocal:         "Appartement",
			SurfaceReelleBati: &s,
			IDParcelle:        fmt.Sprintf("%s-p%d", idPrefix, i),
		}
		if lat != nil && lon != nil {
			mlat := *lat + float64(i)*0.00002
			mlon := *lon + float64(i)*0.00002
			m.Latitude = &mlat
			m.Longitude = &mlon
		}
		resp.Data = append(resp.Data, m)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal mutations: %v", err)
	}
	return b
}

// TestSource_TierFallthrough_FetchesEachSectionOnce pins the per-Query
// fetch memo: the 4 tiers are geographic supersets, so a fall-through
// to the department rung used to re-fetch the primary commune's
// sections up to 4× and every neighbour 2×. With the memo each
// (insee, section) mutation URL must be hit exactly once per Query,
// while the Evidence counters keep recording the winning tier's LOGICAL
// fan-out (3 sections / 3 communes here).
func TestSource_TierFallthrough_FetchesEachSectionOnce(t *testing.T) {
	const (
		primary  = "90001"
		neighbor = "90002"
		deptOnly = "90003"
	)
	bodies := map[string][]byte{
		"/mutations/" + primary + "/000AA":  validMutationsBody(t, 3, "pri", nil, nil),
		"/mutations/" + neighbor + "/000BB": validMutationsBody(t, 3, "ngb", nil, nil),
		"/mutations/" + deptOnly + "/000CC": validMutationsBody(t, 30, "dep", nil, nil),
	}
	counter := newHitCounter()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.inc(r.URL.Path)
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	s := mustNewSource(t, Options{
		HTTP: newHTTPClient(t),
		Communes: stubCommunes{
			neighbors:  []string{primary, neighbor},
			department: []string{primary, neighbor, deptOnly},
		},
	})
	ctx := context.Background()
	for insee, sec := range map[string]string{primary: "000AA", neighbor: "000BB", deptOnly: "000CC"} {
		if err := s.Sections().PrimeFromList(ctx, insee, []string{sec}); err != nil {
			t.Fatalf("PrimeFromList(%s): %v", insee, err)
		}
	}

	// No coords ⇒ address_radius skips fetch-free; commune (3) and
	// neighborhood (3+3) fall below MinSampleSize; department (3+3+30)
	// wins.
	data, err := s.Query(ctx, gazetteer.Listing{
		INSEE:        primary,
		PropertyType: gazetteer.PropertyApartment,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)

	for path := range bodies {
		if got := counter.get(path); got != 1 {
			t.Errorf("GET %s hit %d times, want exactly 1 (per-Query memo)", path, got)
		}
	}
	if got := counter.total(); got != len(bodies) {
		t.Errorf("total mutation GETs = %d, want %d", got, len(bodies))
	}

	// The memo must not change the Evidence numbers: they count the
	// winning tier's logical queries, exactly as a memo-less walk would.
	if res.Evidence.LevelUsed != "department" {
		t.Errorf("LevelUsed = %q, want department", res.Evidence.LevelUsed)
	}
	if res.Evidence.SectionsQueried != 3 {
		t.Errorf("SectionsQueried = %d, want 3 (logical per-tier count)", res.Evidence.SectionsQueried)
	}
	if got, want := fmt.Sprint(res.Evidence.CommunesQueried), fmt.Sprint([]string{primary, neighbor, deptOnly}); got != want {
		t.Errorf("CommunesQueried = %v, want %v", got, want)
	}
	if res.Evidence.RawMutationsCount != 36 {
		t.Errorf("RawMutationsCount = %d, want 36", res.Evidence.RawMutationsCount)
	}
	if res.SampleSize != 36 {
		t.Errorf("SampleSize = %d, want 36", res.SampleSize)
	}
}

// TestSource_GeoCache_ColdThenWarm pins the section-geometry caching:
//
//   - Cold Query: the commune's cadastre GeoJSON is downloaded exactly
//     once, even though the ladder falls through address_radius →
//     commune → … → department (the commune tier's section-code lookup
//     used to re-download the same URL seconds later), and the lone
//     section's mutations are fetched exactly once across all 4 tiers.
//   - Warm Query (same Source, primed kvcache): zero cadastre
//     downloads; only the mutation fetch repeats (mutations are
//     deliberately not cached across Queries).
func TestSource_GeoCache_ColdThenWarm(t *testing.T) {
	const insee = "90001"
	ptLat, ptLon := 48.05, 2.05

	// One section "0000A" whose bbox contains the point.
	cadastreBody := fmt.Sprintf(`{
		"type":"FeatureCollection",
		"features":[
			{"properties":{"commune":%q,"prefixe":"000","code":"A"},
			 "geometry":{"type":"Polygon","coordinates":[[[2.04,48.04],[2.04,48.06],[2.06,48.06],[2.06,48.04],[2.04,48.04]]]}}
		]
	}`, insee)
	cadastreHits := newHitCounter()
	cadastreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cadastreHits.inc(r.URL.Path)
		_, _ = w.Write([]byte(cadastreBody))
	}))
	t.Cleanup(cadastreSrv.Close)
	oldCad := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = cadastreSrv.URL + "/communes"
	t.Cleanup(func() { CadastreSectionsBaseURL = oldCad })

	// 5 in-disk mutations: below MinSampleSizeAddressRadius (12) and
	// MinSampleSize (10), so every rung runs and the unguarded
	// department rung wins with the same 5 rows.
	mutHits := newHitCounter()
	mutBody := validMutationsBody(t, 5, "geo", &ptLat, &ptLon)
	mutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutHits.inc(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mutBody)
	}))
	t.Cleanup(mutSrv.Close)
	withBaseURL(t, mutSrv.URL+"/mutations")

	s := mustNewSource(t, Options{
		HTTP: newHTTPClient(t),
		Communes: stubCommunes{
			neighbors:  []string{insee},
			department: []string{insee},
		},
	})
	// No section priming: the geo download must prime the code list.

	lat, lon := ptLat, ptLon
	listing := gazetteer.Listing{
		INSEE:        insee,
		PropertyType: gazetteer.PropertyApartment,
		Lat:          &lat,
		Lon:          &lon,
	}

	// Cold Query.
	data, err := s.Query(context.Background(), listing)
	if err != nil {
		t.Fatalf("Query (cold): %v", err)
	}
	res := data.(*Result)
	if res.Evidence.LevelUsed != "department" {
		t.Errorf("LevelUsed = %q, want department (5 rows < every guarded floor)", res.Evidence.LevelUsed)
	}
	if res.SampleSize != 5 {
		t.Errorf("SampleSize = %d, want 5", res.SampleSize)
	}
	if got := cadastreHits.total(); got != 1 {
		t.Errorf("cold Query: cadastre GeoJSON downloaded %d times, want exactly 1", got)
	}
	if got := mutHits.get("/mutations/" + insee + "/0000A"); got != 1 {
		t.Errorf("cold Query: section mutations fetched %d times across 4 tiers, want exactly 1", got)
	}

	// Warm Query (fresh per-Query memo, primed kvcache).
	if _, err := s.Query(context.Background(), listing); err != nil {
		t.Fatalf("Query (warm): %v", err)
	}
	if got := cadastreHits.total(); got != 1 {
		t.Errorf("warm Query: cadastre GeoJSON downloads = %d total, want still 1 (zero new)", got)
	}
	if got := mutHits.get("/mutations/" + insee + "/0000A"); got != 2 {
		t.Errorf("warm Query: section mutation fetches = %d total, want 2 (mutations are per-Query fresh)", got)
	}
}

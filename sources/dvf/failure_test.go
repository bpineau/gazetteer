package dvf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

// newFanOutClient is an httpx client fit for a fan-out test against a
// failing fixture: no per-host rate limit and no retries, so every 503
// surfaces at once.
func newFanOutClient(t *testing.T) *httpx.Client {
	t.Helper()
	c, err := httpx.New(httpx.Options{RateLimitPerHost: 1000, MaxRetries: -1})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestSource_AllSectionsFailing_IsAnError pins that an outage is not an
// empty market: when every mutation and cadastre fetch answers 503, the
// walk must not land a successful empty Result at the department tier.
func TestSource_AllSectionsFailing_IsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")
	prevCad := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/cadastre"
	t.Cleanup(func() { CadastreSectionsBaseURL = prevCad })

	s := NewSource(Options{HTTP: newFanOutClient(t)})
	if err := s.Sections().PrimeFromList(context.Background(), "75107", []string{"000AD"}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}
	res, err := s.QueryResult(context.Background(), gazetteer.Listing{
		INSEE:        "75107",
		PropertyType: gazetteer.PropertyApartment,
	})
	if err == nil {
		t.Fatalf("Query = (%+v, nil), want an error: no fetch succeeded", res)
	}
	if !strings.Contains(err.Error(), "cohort is unknown") {
		t.Errorf("err = %v, want the unavailable-cohort error", err)
	}
}

// TestSource_PartialSectionFailure_IsCounted pins that a cohort built
// while some sections stayed unavailable says so in Evidence.
func TestSource_PartialSectionFailure_IsCounted(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/75107/000AD") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	withBaseURL(t, srv.URL+"/mutations")

	s := NewSource(Options{HTTP: newFanOutClient(t)})
	if err := s.Sections().PrimeFromList(context.Background(), "75107", []string{"000AD", "000AE"}); err != nil {
		t.Fatalf("PrimeFromList: %v", err)
	}
	res, err := s.QueryResult(context.Background(), gazetteer.Listing{
		INSEE:        "75107",
		PropertyType: gazetteer.PropertyApartment,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() || res.Evidence.LevelUsed != "commune" {
		t.Fatalf("want a commune-tier reading, got level %q empty=%v", res.Evidence.LevelUsed, res.IsEmpty())
	}
	if res.Evidence.SectionsFailed != 1 {
		t.Errorf("SectionsFailed = %d, want 1 (000AE answered 503)", res.Evidence.SectionsFailed)
	}
}

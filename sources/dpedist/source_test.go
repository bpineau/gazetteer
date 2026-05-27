package dpedist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return body
}

func TestSource_NameVersion(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_BaseURL(t *testing.T) {
	t.Parallel()
	if got := NewSource(Options{}).BaseURL(); got != DefaultBaseURL {
		t.Errorf("BaseURL() default = %q, want %q", got, DefaultBaseURL)
	}
	s := NewSource(Options{BaseURL: "https://example.invalid/api"})
	if got := s.BaseURL(); got != "https://example.invalid/api" {
		t.Errorf("BaseURL() override = %q", got)
	}
}

func TestSource_HappyPath(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "bourg.json")
	hits := atomic.Int32{}
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "01053"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}

	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
	if !strings.Contains(capturedURL, "etiquette_dpe") {
		t.Errorf("URL %q missing field=etiquette_dpe", capturedURL)
	}
	if !strings.Contains(capturedURL, "code_insee_ban") {
		t.Errorf("URL %q missing code_insee_ban filter", capturedURL)
	}
	if !strings.Contains(capturedURL, "01053") {
		t.Errorf("URL %q missing INSEE 01053", capturedURL)
	}

	if res.NbTotal != 13537 {
		t.Errorf("NbTotal = %d, want 13537", res.NbTotal)
	}
	if res.Get(LabelD) != 4249 {
		t.Errorf("LabelD = %d, want 4249", res.Get(LabelD))
	}
	if res.Get(LabelG) != 257 {
		t.Errorf("LabelG = %d, want 257", res.Get(LabelG))
	}
	// 612 F + 257 G = 869, of 13537 = 6.42% → 6.4
	if res.PassoireSharePct < 6.0 || res.PassoireSharePct > 7.0 {
		t.Errorf("PassoireSharePct = %v, want ~6.4", res.PassoireSharePct)
	}
	// 27 A + 464 B = 491, of 13537 = 3.6%
	if res.EfficientSharePct < 3.0 || res.EfficientSharePct > 4.5 {
		t.Errorf("EfficientSharePct = %v, want ~3.6", res.EfficientSharePct)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.IsEmpty() {
		t.Errorf("IsEmpty = true, want false")
	}
	if res.Evidence.INSEE != "01053" {
		t.Errorf("Evidence.INSEE = %q, want 01053", res.Evidence.INSEE)
	}
	if res.Evidence.URL == "" {
		t.Errorf("Evidence.URL empty")
	}

	// Sum of shares ≈ 100 (allow 1.0pp slack for rounding across 7
	// buckets).
	sum := 0.0
	for _, l := range AllLabels {
		sum += res.Share(l)
	}
	if sum < 99.0 || sum > 101.0 {
		t.Errorf("sum of shares = %v, want ~100", sum)
	}
}

func TestSource_EmptyCommune(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "empty.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Error("IsEmpty = false, want true for zero-bucket fixture")
	}
	if res.NbTotal != 0 {
		t.Errorf("NbTotal = %d, want 0", res.NbTotal)
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want empty", res.Confidence)
	}
}

func TestSource_ThinSample(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "thin.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	res, err := Query(context.Background(), Options{BaseURL: srv.URL + "/values_agg"}, gazetteer.Listing{INSEE: "12345"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.NbTotal != 5 {
		t.Errorf("NbTotal = %d, want 5", res.NbTotal)
	}
	if res.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want %q (thin sample)", res.Confidence, ConfidenceLow)
	}
	// 2/5 = 40% F → passoire 40
	if res.PassoireSharePct < 39.0 || res.PassoireSharePct > 41.0 {
		t.Errorf("PassoireSharePct = %v, want ~40", res.PassoireSharePct)
	}
}

func TestSource_NullBucketFoldedToN(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "with_null.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	res, err := Query(context.Background(), Options{BaseURL: srv.URL + "/values_agg"}, gazetteer.Listing{INSEE: "12345"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// 80 D + 10 F + 5 null + 5 total_other → 100
	if res.NbTotal != 100 {
		t.Errorf("NbTotal = %d, want 100 (80D+10F+5null+5other)", res.NbTotal)
	}
	if res.Get(LabelN) != 10 {
		t.Errorf("LabelN = %d, want 10", res.Get(LabelN))
	}
}

func TestSource_NoINSEE_Insufficient(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{BaseURL: "https://example.invalid/values_agg"})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("err = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_Upstream5xx_Transient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75056"})
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_Upstream429_Transient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75056"})
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable (429 retryable)", err)
	}
}

func TestSource_Upstream4xx_Permanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75056"})
	if !errors.Is(err, gazetteer.ErrUpstreamPermanent) {
		t.Errorf("err = %v, want ErrUpstreamPermanent", err)
	}
}

func TestSource_Upstream404_EmptyResult(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75056"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Error("IsEmpty = false, want true on 404")
	}
}

func TestSource_GarbageBody_Transient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/values_agg"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75056"})
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestParse_EmptyBody(t *testing.T) {
	t.Parallel()
	if _, err := Parse(nil); err == nil {
		t.Error("Parse(nil) returned nil error")
	}
	if _, err := Parse([]byte("")); err == nil {
		t.Error("Parse(empty) returned nil error")
	}
}

func TestFoldLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want Label
	}{
		{"A", LabelA},
		{"b", LabelB},
		{"  C  ", LabelC},
		{"G", LabelG},
		{"", LabelN},
		{nil, LabelN},
		{"weird", LabelN},
		{42, LabelN}, // non-string
	}
	for _, c := range cases {
		if got := foldLabel(c.in); got != c.want {
			t.Errorf("foldLabel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestURLForINSEE(t *testing.T) {
	t.Parallel()
	u, err := URLForINSEE("https://example.com/values_agg", "75056")
	if err != nil {
		t.Fatalf("URLForINSEE: %v", err)
	}
	if !strings.Contains(u, "code_insee_ban") {
		t.Errorf("URL %q missing code_insee_ban", u)
	}
	if !strings.Contains(u, "75056") {
		t.Errorf("URL %q missing INSEE", u)
	}
	if !strings.Contains(u, "etiquette_dpe") {
		t.Errorf("URL %q missing etiquette_dpe field", u)
	}
	if _, err := URLForINSEE("https://example.com/api", ""); err == nil {
		t.Error("URLForINSEE(empty insee) returned nil error")
	}
	if _, err := URLForINSEE("", "75056"); err == nil {
		t.Error("URLForINSEE(empty base) returned nil error")
	}
}

func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}

func TestFactoryRoundtrip(t *testing.T) {
	t.Parallel()
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}

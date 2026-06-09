package education

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

// mustReadFixture reads a test fixture from testdata/.
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

func TestSource_HappyPath(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "paris11.json")
	hits := atomic.Int32{}
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75111"})
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
	// We expect Opendatasoft-style where + group_by params on the URL.
	if !strings.Contains(capturedURL, "code_commune") {
		t.Errorf("URL %q missing code_commune filter", capturedURL)
	}
	if !strings.Contains(capturedURL, "OUVERT") {
		t.Errorf("URL %q missing OUVERT etat filter", capturedURL)
	}
	if !strings.Contains(capturedURL, "group_by") {
		t.Errorf("URL %q missing group_by", capturedURL)
	}

	if res.NbEcole != 54 {
		t.Errorf("NbEcole = %d, want 54", res.NbEcole)
	}
	if res.NbCollege != 14 {
		t.Errorf("NbCollege = %d, want 14", res.NbCollege)
	}
	if res.NbLycee != 15 {
		t.Errorf("NbLycee = %d, want 15", res.NbLycee)
	}
	if res.NbMedicoSocial != 6 {
		t.Errorf("NbMedicoSocial = %d, want 6", res.NbMedicoSocial)
	}
	// null + "Service Administratif" → other
	if res.NbOther != 6 {
		t.Errorf("NbOther = %d, want 6 (null+service_admin)", res.NbOther)
	}
	if res.NbTotal != 95 {
		t.Errorf("NbTotal = %d, want 95", res.NbTotal)
	}
	if res.IsEmpty() {
		t.Error("IsEmpty() = true, want false on happy path")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.INSEE != "75111" {
		t.Errorf("Evidence.INSEE = %q, want 75111", res.Evidence.INSEE)
	}
	if res.Evidence.URL == "" {
		t.Error("Evidence.URL empty")
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

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true on empty commune")
	}
	if res.NbTotal != 0 {
		t.Errorf("NbTotal = %d, want 0", res.NbTotal)
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want empty for zero-row case", res.Confidence)
	}
}

func TestSource_NoINSEE_Insufficient(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{BaseURL: "https://example.invalid/records"})
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

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75111"})
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestSource_Upstream4xx_Permanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75111"})
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

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	data, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75111"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res := data.(*Result)
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true on 404")
	}
}

func TestSource_GarbageBody_Transient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	s := NewSource(Options{BaseURL: srv.URL + "/records"})
	_, err := s.Query(context.Background(), gazetteer.Listing{INSEE: "75111"})
	if !errors.Is(err, gazetteer.ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestQuery_TypedHelper(t *testing.T) {
	t.Parallel()
	body := mustReadFixture(t, "paris11.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	res, err := Query(context.Background(), Options{BaseURL: srv.URL + "/records"}, gazetteer.Listing{INSEE: "75111"})
	if err != nil {
		t.Fatalf("Query helper: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Errorf("typed Query returned empty result")
	}
}

func TestFrom_RoundtripFromDossier(t *testing.T) {
	t.Parallel()
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, expected init() to register", Name)
	}
	v := factory()
	if _, ok := v.(*Result); !ok {
		t.Errorf("factory returned %T, want *Result", v)
	}
}

func TestURLForINSEE(t *testing.T) {
	t.Parallel()
	u, err := URLForINSEE("https://example.com/records", "75111")
	if err != nil {
		t.Fatalf("URLForINSEE: %v", err)
	}
	if !strings.Contains(u, "code_commune") {
		t.Errorf("URL %q missing code_commune", u)
	}
	if !strings.Contains(u, "75111") {
		t.Errorf("URL %q missing the INSEE", u)
	}
	if !strings.Contains(u, "OUVERT") {
		t.Errorf("URL %q missing OUVERT", u)
	}
	if !strings.Contains(u, "group_by") {
		t.Errorf("URL %q missing group_by", u)
	}
	// Empty INSEE rejected.
	if _, err := URLForINSEE("https://example.com/records", ""); err == nil {
		t.Error("URLForINSEE(empty insee) returned nil error")
	}
	// Empty base rejected.
	if _, err := URLForINSEE("", "75111"); err == nil {
		t.Error("URLForINSEE(empty base) returned nil error")
	}
}

func TestFoldType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Type
	}{
		{"Ecole", TypeEcole},
		{"école", TypeEcole},
		{"École", TypeEcole},
		{"Collège", TypeCollege},
		{"college", TypeCollege},
		{"Lycée", TypeLycee},
		{"Médico-social", TypeMedicoSoc},
		{"Service Administratif", TypeOther},
		{"", TypeOther},
		{"unknown", TypeOther},
	}
	for _, c := range cases {
		if got := foldType(c.in); got != c.want {
			t.Errorf("foldType(%q) = %q, want %q", c.in, got, c.want)
		}
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

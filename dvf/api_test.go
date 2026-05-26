package dvf

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/pkg/httpx"
)

func TestAPI_GetMutations_HappyPath(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "dvfapi_mutations_75107_AD.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/75107/000AD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	api := NewAPI(hc, nil)
	r, err := api.GetMutations(context.Background(), "75107", "000AD")
	if err != nil {
		t.Fatalf("GetMutations: %v", err)
	}
	if len(r.Data) == 0 {
		t.Fatalf("expected mutations, got 0")
	}
}

// Per-call timeout regression guard. The legacy app.dvf.etalab.gouv.fr API
// used to leave a connection ESTABLISHED for 12+ minutes on some
// (commune, section) pairs. The per-call ctx.WithTimeout(APICallTimeout)
// is kept as belt-and-braces.
func TestAPI_GetMutations_TimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	prev := APICallTimeout
	APICallTimeout = 200 * time.Millisecond
	defer func() { APICallTimeout = prev }()

	hc, _ := httpx.New(httpx.Options{RateLimitPerHost: 1000, MaxRetries: 1, BaseRetryInterval: 10 * time.Millisecond})
	defer func() { _ = hc.Close() }()
	api := NewAPI(hc, nil)

	start := time.Now()
	_, err := api.GetMutations(context.Background(), "93027", "000LZ")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 30*time.Second {
		t.Errorf("call did not abort under per-call timeout: elapsed=%v", elapsed)
	}
	if errors.Is(err, ErrSectionNotFound) {
		t.Errorf("expected timeout, got ErrSectionNotFound")
	}
}

func TestAPI_GetMutations_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	api := NewAPI(hc, nil)
	_, err := api.GetMutations(context.Background(), "00000", "000ZZ")
	if !errors.Is(err, ErrSectionNotFound) {
		t.Errorf("expected ErrSectionNotFound, got %v", err)
	}
}

func TestAPI_NilHTTP(t *testing.T) {
	api := &API{}
	_, err := api.GetMutations(context.Background(), "75107", "000AD")
	if err == nil {
		t.Error("expected error on nil http client")
	}
}

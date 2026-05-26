package dvf

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

func TestDVFSectionCode(t *testing.T) {
	cases := []struct {
		prefixe, code, want string
	}{
		{"000", "AA", "000AA"},
		{"000", "BC", "000BC"},
		{"000", "A", "0000A"},
		{"000", "B", "0000B"},
		{"050", "AB", "050AB"},
		{"000", "ABC", "00ABC"},
		{"", "AA", ""},
		{"000", "", ""},
	}
	for _, c := range cases {
		got := dvfSectionCode(c.prefixe, c.code)
		if got != c.want {
			t.Errorf("dvfSectionCode(%q, %q) = %q want %q", c.prefixe, c.code, got, c.want)
		}
	}
}

func TestFetchCadastreSections_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/communes/93072/geojson/sections") {
			http.Error(w, "wrong path", 404)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.geo+json")
		_, _ = w.Write([]byte(`{
			"type": "FeatureCollection",
			"features": [
				{"id":"930720000A","properties":{"commune":"93072","prefixe":"000","code":"A"}},
				{"id":"930720000B","properties":{"commune":"93072","prefixe":"000","code":"B"}},
				{"id":"930720000A","properties":{"commune":"93072","prefixe":"000","code":"A"}},
				{"id":"930730000A","properties":{"commune":"93073","prefixe":"000","code":"A"}}
			]
		}`))
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	got, err := FetchCadastreSections(context.Background(), hc, "93072")
	if err != nil {
		t.Fatalf("FetchCadastreSections: %v", err)
	}
	sort.Strings(got)
	want := []string{"0000A", "0000B"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestFetchCadastreSections_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":404}`, 404)
	}))
	defer srv.Close()
	old := CadastreSectionsBaseURL
	CadastreSectionsBaseURL = srv.URL + "/communes"
	defer func() { CadastreSectionsBaseURL = old }()

	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	_, err := FetchCadastreSections(context.Background(), hc, "75056")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCadastreCommuneNotFound) {
		t.Errorf("got err=%v, want ErrCadastreCommuneNotFound", err)
	}
}

func TestFetchCadastreSections_NilHTTP(t *testing.T) {
	_, err := FetchCadastreSections(context.Background(), nil, "75107")
	if err == nil {
		t.Fatal("expected error on nil http client")
	}
}

func TestFetchCadastreSections_EmptyINSEE(t *testing.T) {
	hc, _ := httpx.New(httpx.Options{})
	defer func() { _ = hc.Close() }()
	_, err := FetchCadastreSections(context.Background(), hc, "")
	if err == nil {
		t.Fatal("expected error on empty insee")
	}
}

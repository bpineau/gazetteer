package gazetteer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type qtPayload struct{ V int }

type qtSource struct {
	data any
	err  error
}

func (s qtSource) Name() string { return "qt" }
func (s qtSource) Version() int { return 1 }
func (s qtSource) Query(ctx context.Context, l Listing) (any, error) {
	return s.data, s.err
}

func TestQueryTyped(t *testing.T) {
	want := &qtPayload{V: 7}
	got, err := QueryTyped[*qtPayload](context.Background(), qtSource{data: want}, Listing{})
	if err != nil || got != want {
		t.Errorf("QueryTyped = (%v, %v), want (%v, nil)", got, err, want)
	}

	sentinel := errors.New("boom")
	if _, err := QueryTyped[*qtPayload](context.Background(), qtSource{err: sentinel}, Listing{}); !errors.Is(err, sentinel) {
		t.Errorf("error passthrough: got %v, want %v", err, sentinel)
	}

	_, err = QueryTyped[*qtPayload](context.Background(), qtSource{data: "wrong type"}, Listing{})
	if err == nil || !strings.Contains(err.Error(), "qt: typed result mismatch") {
		t.Errorf("type mismatch: got %v, want qt: typed result mismatch", err)
	}
}

func TestListingCoords(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name     string
		lat, lon *float64
		wantOK   bool
	}{
		{"both_set", f(48.85), f(2.35), true},
		{"nil_lat", nil, f(2.35), false},
		{"nil_lon", f(48.85), nil, false},
		{"both_nil", nil, nil, false},
		{"null_island", f(0), f(0), false},
		{"zero_lat_only", f(0), f(2.35), true},
	}
	for _, c := range cases {
		lat, lon, ok := Listing{Lat: c.lat, Lon: c.lon}.Coords()
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if ok && (lat != *c.lat || lon != *c.lon) {
			t.Errorf("%s: coords = (%v, %v), want (%v, %v)", c.name, lat, lon, *c.lat, *c.lon)
		}
	}
}

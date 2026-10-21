package banx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// brokenCache is a kvcache backend whose every call fails, the way a
// persistent store does while its database is down.
type brokenCache struct {
	gets, sets atomic.Int32
}

var errBackendDown = errors.New("backend down")

func (c *brokenCache) Get(context.Context, string) (kvcache.Entry, error) {
	c.gets.Add(1)
	return kvcache.Entry{}, errBackendDown
}

func (c *brokenCache) Set(context.Context, kvcache.Entry) error {
	c.sets.Add(1)
	return errBackendDown
}

func (c *brokenCache) DeleteExpired(context.Context, time.Time) (int64, error) {
	return 0, errBackendDown
}

// stubReverser is a delegate that answers both directions.
type stubReverser struct {
	stubGeocoder
	rev GeocodeResult
}

func (s *stubReverser) Reverse(context.Context, float64, float64) (GeocodeResult, error) {
	s.calls.Add(1)
	return s.rev, nil
}

// A cache is an optimisation: a backend that cannot be read must cost the
// caller a delegate round-trip, never the answer. A read error used to fail
// the whole geocode (and every Source behind it) while the delegate had the
// address; a write error was swallowed in silence. Both now degrade the
// same way: logged at Warn, the delegate's answer served.
func TestCachedGeocoder_BrokenBackendDegradesToDelegate(t *testing.T) {
	t.Parallel()

	want := GeocodeResult{Label: "1 Rue Exemple 93200 Saint-Denis", CityCode: "93066", PostCode: "93200", Lat: 48.93, Lon: 2.35}
	delegate := &stubReverser{stubGeocoder: stubGeocoder{res: want}, rev: want}
	cache := &brokenCache{}
	var logs bytes.Buffer
	g := NewCachedGeocoder(delegate, cache, 0).WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	got, err := g.Geocode(context.Background(), GeocodeQuery{Address: "1 rue Exemple", Zip: "93200"})
	if err != nil {
		t.Fatalf("Geocode with an unreadable cache: %v, want the delegate's answer", err)
	}
	if got.CityCode != want.CityCode {
		t.Errorf("CityCode = %q, want %q", got.CityCode, want.CityCode)
	}

	rgot, err := g.Reverse(context.Background(), 48.93, 2.35)
	if err != nil {
		t.Fatalf("Reverse with an unreadable cache: %v, want the delegate's answer", err)
	}
	if rgot.CityCode != want.CityCode {
		t.Errorf("Reverse CityCode = %q, want %q", rgot.CityCode, want.CityCode)
	}

	if n := delegate.calls.Load(); n != 2 {
		t.Errorf("delegate calls = %d, want 2", n)
	}
	if n := cache.sets.Load(); n != 2 {
		t.Errorf("cache writes attempted = %d, want 2", n)
	}
	out := logs.String()
	for _, want := range []string{"geocode.cache.read_failed", "geocode.cache.write_failed"} {
		if strings.Count(out, want) != 2 {
			t.Errorf("want %q logged once per direction, logs:\n%s", want, out)
		}
	}
}

// A cancelled context is the caller giving up, not a backend fault: it must
// still end the call rather than be laundered into a delegate request.
func TestCachedGeocoder_CancelledContextStillFails(t *testing.T) {
	t.Parallel()

	delegate := &stubGeocoder{res: GeocodeResult{CityCode: "93066", PostCode: "93200"}}
	g := NewCachedGeocoder(delegate, &cancelledCache{}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Geocode(ctx, GeocodeQuery{Address: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := delegate.calls.Load(); n != 0 {
		t.Errorf("delegate called %d times after cancellation", n)
	}
}

// cancelledCache reports the context's error, as a real backend does.
type cancelledCache struct{ brokenCache }

func (*cancelledCache) Get(ctx context.Context, _ string) (kvcache.Entry, error) {
	return kvcache.Entry{}, ctx.Err()
}

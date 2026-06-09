package gazetteer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBuilder_Defaults(t *testing.T) {
	b := NewBuilder()
	if b == nil {
		t.Fatal("NewBuilder returned nil")
	}
	if b.httpClient == nil {
		t.Errorf("default httpClient is nil")
	}
	if b.logger == nil {
		t.Errorf("default logger is nil")
	}
}

func TestBuilder_With(t *testing.T) {
	b := NewBuilder()
	s := &fakeSource{name: "a", ver: 1}
	if b.With(s) != b {
		t.Errorf("With did not return the same builder for chaining")
	}
	if len(b.sources) != 1 || b.sources[0] != s {
		t.Errorf("sources = %v, want [a]", b.sources)
	}
}

func TestBuilder_Build_ReturnsClient(t *testing.T) {
	b := NewBuilder().With(&fakeSource{name: "a", ver: 1})
	c, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c == nil {
		t.Fatal("Build returned nil client")
	}
}

func TestBuilder_Build_DuplicateNamesError(t *testing.T) {
	b := NewBuilder().
		With(&fakeSource{name: "a", ver: 1}).
		With(&fakeSource{name: "a", ver: 2})
	if _, err := b.Build(); err == nil {
		t.Errorf("Build should error on duplicate source names")
	}
}

func TestBuilder_OptionSetters(t *testing.T) {
	customHC := &http.Client{}
	customLog := slog.Default().With("x", "y")

	b := NewBuilder().
		WithHTTPClient(customHC).
		WithLogger(customLog).
		WithMaxConcurrency(3).
		WithNormalizer(&fakeNormalizer{})

	if b.httpClient != customHC {
		t.Errorf("WithHTTPClient did not apply")
	}
	if b.logger != customLog {
		t.Errorf("WithLogger did not apply")
	}
	if b.maxConcur != 3 {
		t.Errorf("WithMaxConcurrency = %d, want 3", b.maxConcur)
	}
	if b.normalizer == nil {
		t.Errorf("WithNormalizer did not apply")
	}
}

func TestClient_CollectStub(t *testing.T) {
	// Confirms Collect exists and returns a Dossier with at least the
	// listing echoed. Full behaviour tested in Task 14.
	c, _ := NewBuilder().Build()
	d := c.Collect(context.Background(), Listing{Address: "x"})
	if d.Listing.Address != "x" {
		t.Errorf("Dossier.Listing.Address = %q, want %q", d.Listing.Address, "x")
	}
	if d.Results == nil {
		t.Errorf("Dossier.Results should never be nil")
	}
}

type fakeEmptyPayload struct{ Empty bool }

func (f *fakeEmptyPayload) IsEmpty() bool { return f.Empty }

func TestCollect_RunsAllSourcesInParallel(t *testing.T) {
	var concurrency atomic.Int32
	var peakConcurrency atomic.Int32
	mkSource := func(name string) *fakeSource {
		return &fakeSource{
			name: name,
			ver:  1,
			out:  &fakeEmptyPayload{},
		}
	}
	wrap := func(s *fakeSource) Source {
		return &concurrencyTrackingSource{
			Source: s, in: &concurrency, peak: &peakConcurrency,
		}
	}

	c, _ := NewBuilder().
		With(wrap(mkSource("a"))).
		With(wrap(mkSource("b"))).
		With(wrap(mkSource("c"))).
		Build()
	d := c.Collect(context.Background(), Listing{})
	if len(d.Results) != 3 {
		t.Fatalf("Results count = %d, want 3", len(d.Results))
	}
	if peakConcurrency.Load() < 2 {
		t.Errorf("peak concurrency = %d, expected ≥ 2 (sources should run in parallel)", peakConcurrency.Load())
	}
}

type concurrencyTrackingSource struct {
	Source
	in   *atomic.Int32
	peak *atomic.Int32
}

func (c *concurrencyTrackingSource) Query(ctx context.Context, l Listing) (any, error) {
	n := c.in.Add(1)
	defer c.in.Add(-1)
	for {
		old := c.peak.Load()
		if n <= old || c.peak.CompareAndSwap(old, n) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	return c.Source.Query(ctx, l)
}

func TestCollect_StatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		out    any
		err    error
		status Status
	}{
		{"ok", &fakeEmptyPayload{Empty: false}, nil, StatusOK},
		{"empty", &fakeEmptyPayload{Empty: true}, nil, StatusOKEmpty},
		{"skipped_inputs", nil, ErrInsufficientInputs, StatusSkippedPrereq},
		{"skipped_proptype", nil, ErrUnsupportedPropertyType, StatusSkippedPrereq},
		{"antibot", nil, ErrAntiBot, StatusFailedAntiBot},
		{"outdated", nil, ErrUpstreamSchemaChanged, StatusFailedOutdated},
		{"permanent", nil, ErrUpstreamPermanent, StatusFailedPermanent},
		{"transient", nil, errors.New("network blip"), StatusFailedTransient},
		{"transient_wrapped", nil, fmt.Errorf("wrap: %w", ErrUpstreamUnavailable), StatusFailedTransient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := &fakeSource{name: c.name, ver: 1, out: c.out, err: c.err}
			cl, _ := NewBuilder().With(src).Build()
			d := cl.Collect(context.Background(), Listing{})
			got := d.Results[c.name].Status
			if got != c.status {
				t.Errorf("Status = %v, want %v", got, c.status)
			}
		})
	}
}

func TestCollect_RespectsContextCancellation(t *testing.T) {
	slow := &slowSource{delay: time.Second}
	c, _ := NewBuilder().With(slow).Build()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	d := c.Collect(ctx, Listing{})
	if d.Results["slow"].Status == StatusOK {
		t.Errorf("Status = OK after immediate cancel; want failure")
	}
}

type slowSource struct{ delay time.Duration }

func (s *slowSource) Name() string { return "slow" }
func (s *slowSource) Version() int { return 1 }
func (s *slowSource) Query(ctx context.Context, _ Listing) (any, error) {
	select {
	case <-time.After(s.delay):
		return &fakeEmptyPayload{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// namedSource is a no-op Source identified only by its Name, for roster tests.
type namedSource string

func (n namedSource) Name() string                                { return string(n) }
func (n namedSource) Version() int                                { return 1 }
func (n namedSource) Query(context.Context, Listing) (any, error) { return &fakeEmptyPayload{}, nil }

func TestBuilderWithout(t *testing.T) {
	c, err := NewBuilder().
		With(namedSource("a")).
		With(namedSource("b")).
		With(namedSource("c")).
		Without("b", "absent"). // "absent" is a no-op; "b" is dropped
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := make(map[string]bool, len(c.sources))
	for _, s := range c.sources {
		got[s.Name()] = true
	}
	if !got["a"] || !got["c"] {
		t.Fatalf("Without dropped a kept source: have %v, want a and c", got)
	}
	if got["b"] {
		t.Fatalf("Without did not drop %q: have %v", "b", got)
	}
	if len(c.sources) != 2 {
		t.Fatalf("roster size = %d, want 2 (a, c)", len(c.sources))
	}

	// No-op: Without() with no names returns the roster unchanged.
	c2, _ := NewBuilder().With(namedSource("x")).Without().Build()
	if len(c2.sources) != 1 {
		t.Fatalf("Without() with no names changed the roster: size %d, want 1", len(c2.sources))
	}
}

func TestCollectSome(t *testing.T) {
	c, err := NewBuilder().
		With(namedSource("a")).
		With(namedSource("b")).
		With(namedSource("c")).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	d := c.CollectSome(context.Background(), Listing{}, "a", "c", "absent")
	if _, ok := d.Results["a"]; !ok {
		t.Errorf("CollectSome dropped requested source %q", "a")
	}
	if _, ok := d.Results["c"]; !ok {
		t.Errorf("CollectSome dropped requested source %q", "c")
	}
	if _, ok := d.Results["b"]; ok {
		t.Errorf("CollectSome ran unrequested source %q", "b")
	}
	if len(d.Results) != 2 {
		t.Errorf("CollectSome ran %d sources, want 2 (a, c)", len(d.Results))
	}
	// No names ⇒ empty partial Dossier (runs nothing).
	if e := c.CollectSome(context.Background(), Listing{}); len(e.Results) != 0 {
		t.Errorf("CollectSome() with no names ran %d sources, want 0", len(e.Results))
	}
}

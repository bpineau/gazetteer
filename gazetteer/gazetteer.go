package gazetteer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Builder configures a Client. Use NewBuilder, chain With* methods, call
// Build. After Build the Builder may be discarded.
type Builder struct {
	sources    []Source
	httpClient *http.Client
	logger     *slog.Logger
	debugDump  bool
	maxConcur  int
	normalizer Normalizer
}

// NewBuilder returns a Builder pre-populated with sane defaults
// (http.DefaultClient, slog.Default(); Cache falls back to the
// package-level default via CacheFrom). Override any default with the
// corresponding With* method.
func NewBuilder() *Builder {
	return &Builder{
		httpClient: http.DefaultClient,
		logger:     slog.Default(),
	}
}

// With adds a Source to the Builder. Returns the Builder for chaining.
func (b *Builder) With(s Source) *Builder {
	b.sources = append(b.sources, s)
	return b
}

// WithHTTPClient overrides the default HTTP client propagated to Sources.
//
// The client is stored on ctx via WithHTTPClient when Client.Collect runs;
// each Source reads it via HTTPClientFrom(ctx). Sources MAY still override
// with their own Options.HTTPClient — see WithHTTPClient docstring for the
// precedence rule across the shipped Sources.
func (b *Builder) WithHTTPClient(c *http.Client) *Builder {
	b.httpClient = c
	return b
}

// WithLogger overrides the default slog.Logger propagated to Sources.
func (b *Builder) WithLogger(l *slog.Logger) *Builder {
	b.logger = l
	return b
}

// WithDebugDump enables raw request/response logging for sources that
// honour the flag.
func (b *Builder) WithDebugDump(on bool) *Builder {
	b.debugDump = on
	return b
}

// WithMaxConcurrency caps the number of Sources executed in parallel by
// Client.Collect. Zero or negative = unlimited.
func (b *Builder) WithMaxConcurrency(n int) *Builder {
	b.maxConcur = n
	return b
}

// WithNormalizer installs a Normalizer on the Builder. The built
// Client exposes a Normalize method that delegates to it. When no
// Normalizer is installed, Client.Normalize returns
// ErrNormalizerNotConfigured.
func (b *Builder) WithNormalizer(n Normalizer) *Builder {
	b.normalizer = n
	return b
}

// Build finalises the configuration and returns an immutable Client.
// Errors when two Sources share a Name (a programming error).
func (b *Builder) Build() (*Client, error) {
	names := make(map[string]bool, len(b.sources))
	for _, s := range b.sources {
		if names[s.Name()] {
			return nil, fmt.Errorf("gazetteer: duplicate Source name %q", s.Name())
		}
		names[s.Name()] = true
	}
	c := &Client{
		sources:    append([]Source(nil), b.sources...),
		httpClient: b.httpClient,
		logger:     b.logger,
		debugDump:  b.debugDump,
		maxConcur:  b.maxConcur,
		normalizer: b.normalizer,
	}
	return c, nil
}

// Client is the immutable, ready-to-use compiler of Dossiers.
type Client struct {
	sources    []Source
	httpClient *http.Client
	logger     *slog.Logger
	debugDump  bool
	maxConcur  int
	normalizer Normalizer
}

// Collect runs every configured Source in parallel, populates a Dossier
// with one Result per Source, and returns. Errors from individual
// Sources are translated to Result.Status via classifyErr; Collect
// itself does not return an error.
//
// Concurrency is unlimited unless WithMaxConcurrency was set. The shared
// ctx propagates HTTP client / logger / debug-dump to each Source via
// the context-key helpers. Sources that need a persistent kvcache.Cache
// receive it via their own Options field instead of a framework slot.
func (c *Client) Collect(ctx context.Context, l Listing) Dossier {
	started := time.Now()

	ctx = WithHTTPClient(ctx, c.httpClient)
	ctx = WithLogger(ctx, c.logger)
	ctx = WithDebugDump(ctx, c.debugDump)

	var sem chan struct{}
	if c.maxConcur > 0 {
		sem = make(chan struct{}, c.maxConcur)
	}

	results := make(map[string]Result, len(c.sources))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, s := range c.sources {
		wg.Go(func() {
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			r := runOne(ctx, s, l)
			mu.Lock()
			results[s.Name()] = r
			mu.Unlock()
		})
	}
	wg.Wait()

	return Dossier{
		Listing:    l,
		Results:    results,
		StartedAt:  started,
		FinishedAt: time.Now(),
	}
}

func runOne(ctx context.Context, s Source, l Listing) Result {
	r := Result{
		Name:      s.Name(),
		Version:   s.Version(),
		FetchedAt: time.Now(),
	}
	data, err := s.Query(ctx, l)
	if err != nil {
		r.Err = err
		r.Status = classifyErr(err)
		return r
	}
	r.Data = data
	r.Status = StatusOK
	if er, ok := data.(EmptyReporter); ok && er.IsEmpty() {
		r.Status = StatusOKEmpty
	}
	if ev, ok := data.(Evidencer); ok {
		r.Evidence = ev.Evidence()
	}
	return r
}

func classifyErr(err error) Status {
	switch {
	case errors.Is(err, ErrInsufficientInputs), errors.Is(err, ErrUnsupportedPropertyType):
		return StatusSkippedPrereq
	case errors.Is(err, ErrAntiBot):
		return StatusFailedAntiBot
	case errors.Is(err, ErrUpstreamSchemaChanged):
		return StatusFailedOutdated
	case errors.Is(err, ErrUpstreamPermanent):
		return StatusFailedPermanent
	default:
		return StatusFailedTransient
	}
}

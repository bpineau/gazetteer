package httpx

import (
	"context"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// rateLimitTransport is the innermost-but-one layer: it waits on a
// per-host token-bucket before delegating to the next RoundTripper.
// It is wrapped by retryTransport above (so retries also wait), and
// itself wraps the stdlib transport below.
//
// Concurrency: limiters is a sync.Map keyed by host. The slow path of
// limiterFor uses LoadOrStore to dedupe construction under contention,
// which is the documented safe pattern. No external mutex is needed.
type rateLimitTransport struct {
	next     http.RoundTripper
	resolved resolved

	limiters sync.Map // map[host]*rate.Limiter
}

func newRateLimitTransport(next http.RoundTripper, r resolved) *rateLimitTransport {
	return &rateLimitTransport{next: next, resolved: r}
}

// RoundTrip blocks on the host-specific limiter, then forwards.
func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.wait(req.Context(), req.URL.Host); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}

// wait blocks until the host limiter grants a token or ctx is done.
func (t *rateLimitTransport) wait(ctx context.Context, host string) error {
	lim := t.limiterFor(host)
	return lim.Wait(ctx)
}

func (t *rateLimitTransport) limiterFor(host string) *rate.Limiter {
	if v, ok := t.limiters.Load(host); ok {
		return v.(*rate.Limiter)
	}

	// Slow path: build the limiter under a guard to avoid creating two
	// instances for the same host under contention. sync.Map's
	// LoadOrStore handles the race correctly but constructing a Limiter
	// is cheap enough we don't bother with sync.Once.
	rl, burst := t.hostConfig(host)
	lim := rate.NewLimiter(rate.Limit(rl), burst)
	actual, _ := t.limiters.LoadOrStore(host, lim)
	return actual.(*rate.Limiter)
}

// hostConfig resolves (rate, burst) for the given host, applying PerHost
// overrides on top of the global defaults.
func (t *rateLimitTransport) hostConfig(host string) (float64, int) {
	rl := t.resolved.rateLimit
	burst := t.resolved.burst
	if t.resolved.perHost != nil {
		if h, ok := t.resolved.perHost[host]; ok {
			if h.RateLimit != nil && *h.RateLimit > 0 {
				rl = *h.RateLimit
			}
			if h.Burst != nil && *h.Burst > 0 {
				burst = *h.Burst
			}
		}
	}
	return rl, burst
}

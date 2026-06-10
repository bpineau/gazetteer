package httpx

import (
	"log/slog"
	"net/http"
	"time"
)

// Default values for Options.
const (
	// DefaultUserAgent matches a Chrome 147 / macOS UA captured from a
	// real browser. We mimic a vanilla desktop Chrome rather
	// than identifying as a custom UA because (a) DataDome / similar
	// anti-bot stacks score unknown UAs aggressively, and (b) every
	// scraped site we hit is fine with desktop Chrome traffic. Bump the
	// Chrome major version when the operator's local Chrome bumps.
	DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/147.0.0.0 Safari/537.36"
	DefaultRateLimitPerHost   = 2.0
	DefaultBurstPerHost       = 4
	DefaultMaxRetries         = 5
	DefaultBaseRetryInterval  = 500 * time.Millisecond
	DefaultMaxRetryAfter      = 300 * time.Second
	DefaultBackoffCap         = 60 * time.Second
	DefaultTTLFallback        = 6 * time.Hour
	DefaultMaxResponseBytes   = int64(50 * 1024 * 1024)
	defaultClientTimeout      = 60 * time.Second
	defaultDownloadTimeout    = 15 * time.Minute
	defaultErrBodySnippetSize = 4 * 1024
)

// Options configures a Client. Zero-valued fields take sensible defaults
// (see DefaultXxx constants). Fields are documented in the spec.
type Options struct {
	// UserAgent is sent on every outgoing request. A per-host override
	// from PerHost takes precedence.
	UserAgent string

	// HTTPCacheDir is the on-disk directory for the persistent HTTP cache.
	// Empty disables caching entirely.
	HTTPCacheDir string

	// SnapshotDir is the on-disk directory for raw request/response
	// snapshots. Empty disables snapshotting (unless WithSnapshot is used
	// per-request).
	SnapshotDir string

	// RateLimitPerHost is the steady-state token-bucket refill rate, in
	// requests-per-second. Defaults to DefaultRateLimitPerHost.
	RateLimitPerHost float64

	// BurstPerHost is the token-bucket maximum burst. Defaults to
	// DefaultBurstPerHost.
	BurstPerHost int

	// MaxRetries is the cap on retry attempts (after the initial one).
	// Total attempts = MaxRetries + 1. Defaults to DefaultMaxRetries.
	//
	// Convention :
	//   - 0   (zero value) means "use the default" (DefaultMaxRetries).
	//   - n>0 means "retry up to n times after the initial attempt".
	//   - n<0 (e.g. -1) means "no retries at all, one attempt only".
	//
	// Tests that expect a single attempt against a 4xx/5xx fixture must
	// use -1 ; otherwise the default 5 retries with exponential backoff
	// (500 ms × 2^n, capped at 60 s) compound into multi-second waits.
	MaxRetries int

	// BaseRetryInterval is the unit used by the exponential backoff:
	// delay_n = base × 2^n + rand[0, base). Defaults to 500ms.
	BaseRetryInterval time.Duration

	// MaxRetryAfter caps any server-supplied Retry-After header so a
	// poorly-behaved site can't pin us for hours. Defaults to 300s.
	MaxRetryAfter time.Duration

	// BackoffCap caps the computed backoff regardless of the formula.
	// Defaults to 60s.
	BackoffCap time.Duration

	// DefaultTTL is the cache TTL applied when the server provides no
	// Cache-Control / max-age and no ETag/Last-Modified validators.
	DefaultTTL time.Duration

	// MaxResponseBytes guards against runaway downloads. 0 = unlimited.
	// Defaults to DefaultMaxResponseBytes (50 MiB).
	MaxResponseBytes int64

	// PerHost holds host-keyed overrides (key = u.Host with port preserved).
	PerHost map[string]HostOptions

	// Logger receives structured debug/warn/error events. Defaults to
	// slog.Default with comp=httpx.
	Logger *slog.Logger

	// Now is the time source, injectable for tests. Defaults to time.Now.
	Now func() time.Time

	// Transport is the inner transport (the most-internal layer of the
	// composite). Defaults to a fresh http.DefaultTransport clone.
	Transport http.RoundTripper

	// CookieJar is an optional cookie jar passed through to *http.Client.
	// Sources that need it (login flows) supply one.
	CookieJar http.CookieJar
}

// HostOptions holds per-host overrides. Pointer fields distinguish
// "explicitly set" from "absent": a nil override means "inherit Options".
type HostOptions struct {
	RateLimit  *float64
	Burst      *int
	DefaultTTL *time.Duration
	UserAgent  *string
	Headers    http.Header
}

// resolved is the post-defaults snapshot of Options used internally.
type resolved struct {
	userAgent         string
	cacheDir          string
	snapshotDir       string
	rateLimit         float64
	burst             int
	maxRetries        int
	baseRetryInterval time.Duration
	maxRetryAfter     time.Duration
	backoffCap        time.Duration
	defaultTTL        time.Duration
	maxResponseBytes  int64
	perHost           map[string]HostOptions
	logger            *slog.Logger
	now               func() time.Time
	innerTransport    http.RoundTripper
	cookieJar         http.CookieJar
}

func (o Options) resolve() resolved {
	r := resolved{
		userAgent:         firstNonEmpty(o.UserAgent, DefaultUserAgent),
		cacheDir:          o.HTTPCacheDir,
		snapshotDir:       o.SnapshotDir,
		rateLimit:         o.RateLimitPerHost,
		burst:             o.BurstPerHost,
		maxRetries:        o.MaxRetries,
		baseRetryInterval: o.BaseRetryInterval,
		maxRetryAfter:     o.MaxRetryAfter,
		backoffCap:        o.BackoffCap,
		defaultTTL:        o.DefaultTTL,
		maxResponseBytes:  o.MaxResponseBytes,
		perHost:           o.PerHost,
		logger:            o.Logger,
		now:               o.Now,
		innerTransport:    o.Transport,
		cookieJar:         o.CookieJar,
	}

	if r.rateLimit <= 0 {
		r.rateLimit = DefaultRateLimitPerHost
	}
	if r.burst <= 0 {
		r.burst = DefaultBurstPerHost
	}
	if r.maxRetries < 0 {
		r.maxRetries = 0
	} else if o.MaxRetries == 0 {
		r.maxRetries = DefaultMaxRetries
	}
	if r.baseRetryInterval <= 0 {
		r.baseRetryInterval = DefaultBaseRetryInterval
	}
	if r.maxRetryAfter <= 0 {
		r.maxRetryAfter = DefaultMaxRetryAfter
	}
	if r.backoffCap <= 0 {
		r.backoffCap = DefaultBackoffCap
	}
	if r.defaultTTL <= 0 {
		r.defaultTTL = DefaultTTLFallback
	}
	if r.maxResponseBytes < 0 {
		r.maxResponseBytes = 0
	} else if o.MaxResponseBytes == 0 {
		r.maxResponseBytes = DefaultMaxResponseBytes
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	r.logger = r.logger.With(slog.String("comp", "httpx"))

	if r.innerTransport == nil {
		// Clone the default transport so users can configure dial/TLS
		// timeouts without touching the global instance.
		t := http.DefaultTransport.(*http.Transport).Clone()
		r.innerTransport = t
	}

	return r
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// Response is the public, body-less view of a completed request, returned
// by GetBytes alongside the body. *http.Response is intentionally not
// exported (it'd leak an undrained body).
type Response struct {
	Status      int
	Header      http.Header
	DurationMs  int64
	FromCache   bool
	Attempts    int
	URL         string
	ContentType string
	BodyBytes   int64
}

// DownloadOptions tunes Client.Download.
type DownloadOptions struct {
	// ExpectedSHA256, if non-empty, is checked after writing. Mismatch =>
	// the file is removed and a non-nil error is returned.
	ExpectedSHA256 string
	// SkipIfExists short-circuits the network when destPath already
	// exists; the existing file's sha256 is computed and returned.
	SkipIfExists bool
	// MaxBytes guards against bombs (0 = unlimited / inherit Options).
	MaxBytes int64
	// Timeout caps the whole download (connect + full body stream).
	// 0 = the 15-minute default. Downloads deliberately bypass the
	// client-wide request Timeout (60 s by default): that deadline spans
	// the entire body read, which is wrong for arbitrarily large raw
	// files — a multi-hundred-MB CSV on a slow mirror is healthy long
	// past 60 s.
	Timeout time.Duration
}

// DownloadResult is what Client.Download returns on success.
type DownloadResult struct {
	SHA256  string
	Bytes   int64
	Cached  bool
	Skipped bool
	Path    string
}

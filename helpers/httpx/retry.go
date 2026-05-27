package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

// retryTransport wraps the inner transport and retries on:
//   - transport errors that are deemed retryable (DNS, dial, EOF, reset…)
//   - HTTP 408, 429, and any 5xx except 501.
//
// It honours Retry-After headers (capped to MaxRetryAfter), and uses an
// exponential backoff with additive jitter elsewhere.
type retryTransport struct {
	next     http.RoundTripper
	resolved resolved
}

func newRetryTransport(next http.RoundTripper, r resolved) *retryTransport {
	return &retryTransport{next: next, resolved: r}
}

// RoundTrip implements http.RoundTripper.
//
// If the request has a body, we buffer it once so each retry can replay
// from a fresh reader. This is necessary because http.Request.Body is a
// one-shot io.ReadCloser.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, &ErrTransport{URL: req.URL.String(), Err: err}
		}
	}

	resetBody := func(r *http.Request) {
		if bodyBytes == nil {
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		r.ContentLength = int64(len(bodyBytes))
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	ctx := req.Context()
	maxAttempts := t.resolved.maxRetries + 1
	var lastErr error

	for attempt := range maxAttempts {
		// Replay body for every attempt.
		reqCopy := req.Clone(ctx)
		resetBody(reqCopy)

		resp, err := t.next.RoundTrip(reqCopy)

		// Decide retry vs return.
		retry, retryAfter, reason := t.shouldRetry(resp, err)
		if !retry {
			// Either success or a non-retryable error: return as-is.
			// On error, we wrap transport errors here for clarity.
			if err != nil {
				return nil, &ErrTransport{URL: req.URL.String(), Err: err}
			}
			return resp, nil
		}

		// Retryable: drain+close any partial response so we can reuse the conn.
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		// Record an error to bubble up if we exhaust retries.
		if err != nil {
			lastErr = &ErrTransport{URL: req.URL.String(), Err: err}
		} else {
			lastErr = &ErrHTTP{Status: resp.StatusCode, URL: req.URL.String()}
		}

		// Last attempt? Don't sleep, just bail.
		if attempt == maxAttempts-1 {
			break
		}

		// Compute the wait duration: prefer server-supplied Retry-After
		// (capped), fall back to exp backoff + additive jitter.
		var wait time.Duration
		if retryAfter > 0 {
			wait = min(retryAfter, t.resolved.maxRetryAfter)
		} else {
			wait = t.backoff(attempt)
		}

		// A single transport-error retry on the first attempt is
		// noise in the wild (HTTP/2 RST_STREAM, idle-conn close,
		// TLS hiccup — all common on long-lived public APIs).
		// Demote it to DEBUG ; the retry succeeds on the second
		// attempt 9 times out of 10 and a WARN per request would
		// flood operator dashboards with no actionable signal.
		// Anything past the first retry — or any non-transport
		// reason (4xx / 5xx) — stays WARN: those reflect a real
		// upstream problem the operator should see.
		level := slog.LevelWarn
		if attempt == 0 && reason == "transport-error" {
			level = slog.LevelDebug
		}
		t.resolved.logger.Log(ctx, level, "retrying request",
			slog.String("url", req.URL.String()),
			slog.Int("attempt", attempt+1),
			slog.Int("max_attempts", maxAttempts),
			slog.String("reason", reason),
			slog.Duration("wait", wait),
		)

		// Sleep, but yield to ctx cancellation.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	return nil, &ErrTooManyRetries{
		URL:      req.URL.String(),
		Attempts: maxAttempts,
		Err:      lastErr,
	}
}

// shouldRetry inspects the response/error pair and decides.
// Returns (retry, retryAfterDuration, reason). retryAfter is 0 when the
// caller should fall back to the backoff formula.
func (t *retryTransport) shouldRetry(resp *http.Response, err error) (bool, time.Duration, string) {
	if err != nil {
		// Transport-layer error: retry if it looks transient.
		return isRetryableNetErr(err), 0, "transport-error"
	}
	if resp == nil {
		return false, 0, ""
	}

	switch resp.StatusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests: // 429
		return true, parseRetryAfter(resp.Header.Get("Retry-After"), t.resolved.now()), "status-" + strconv.Itoa(resp.StatusCode)
	}

	if resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented {
		return true, parseRetryAfter(resp.Header.Get("Retry-After"), t.resolved.now()), "status-" + strconv.Itoa(resp.StatusCode)
	}

	return false, 0, ""
}

// backoff returns the wait duration before attempt n+1 (0-indexed).
// Formula: base × 2^n + rand[0, base), capped at BackoffCap.
func (t *retryTransport) backoff(attempt int) time.Duration {
	base := t.resolved.baseRetryInterval
	// Guard against absurd left-shift.
	shift := min(attempt, 16)
	d := base << shift
	jitter := time.Duration(rand.Int64N(int64(base))) //nolint:gosec // retry backoff jitter; non-cryptographic by design
	d += jitter
	if d > t.resolved.backoffCap {
		d = t.resolved.backoffCap
	}
	return d
}

// parseRetryAfter parses an HTTP Retry-After header (RFC 9110), accepting
// either a delta-seconds integer or an HTTP-date. Returns 0 on parse failure.
func parseRetryAfter(s string, now time.Time) time.Duration {
	if s == "" {
		return 0
	}
	// delta-seconds form
	if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form
	if t, err := http.ParseTime(s); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// isRetryableNetErr returns true for errors that have a reasonable chance
// of succeeding on retry: DNS lookup, connection reset, EOF, deadline
// exceeded (when not from the user's ctx), generic net.Error.Timeout etc.
func isRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}
	// User-cancelled contexts must not be retried.
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if netErr, ok := errors.AsType[net.Error](err); ok {
		if netErr.Timeout() {
			return true
		}
	}
	// Otherwise, default to retryable: transport-level failures from
	// http.Transport are typically transient (broken pipe, refused…).
	return true
}

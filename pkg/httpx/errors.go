// Package httpx — error types.
//
// Three categories cover the failure modes a caller cares about:
//   - ErrHTTP        : the server answered, but with a non-2xx status (4xx or 5xx).
//   - ErrTransport   : the request never reached a server cleanly (DNS, dial, TLS, EOF…).
//   - ErrTooManyRetries : we exhausted MaxRetries; wraps the last underlying error.
//
// All three implement error and play well with errors.Is / errors.As. Use
// errors.As to recover the typed value when you need Status / Body.
package httpx

import (
	"errors"
	"fmt"
)

// ErrHTTP is returned when the server responded with an unsuccessful HTTP
// status code (>= 400) and we decided not to retry (or ran out of retries
// for non-retryable codes). Status is the HTTP status code; Body is a
// (possibly truncated) snippet of the response body, useful for logs.
type ErrHTTP struct {
	Status int
	URL    string
	Body   []byte // capped to a few KiB for log safety; full body lives in the cache/snapshot
}

// Error implements the error interface.
func (e *ErrHTTP) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("httpx: HTTP %d for %s", e.Status, e.URL)
}

// ErrTransport wraps a low-level transport error (DNS, dial, TLS, EOF…).
// Use errors.Unwrap or errors.Is on the wrapped error to inspect.
type ErrTransport struct {
	URL string
	Err error
}

// Error implements the error interface.
func (e *ErrTransport) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("httpx: transport error for %s: %v", e.URL, e.Err)
}

// Unwrap exposes the underlying error to errors.Is / errors.As.
func (e *ErrTransport) Unwrap() error { return e.Err }

// ErrTooManyRetries means we exhausted Options.MaxRetries. The wrapped Err
// is the last attempt's error (typically *ErrHTTP or *ErrTransport).
type ErrTooManyRetries struct {
	URL      string
	Attempts int
	Err      error
}

// Error implements the error interface.
func (e *ErrTooManyRetries) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("httpx: %d attempts exhausted for %s: %v", e.Attempts, e.URL, e.Err)
}

// Unwrap chains the last underlying error.
func (e *ErrTooManyRetries) Unwrap() error { return e.Err }

// asHTTPStatus extracts the HTTP status code from err if it is (or wraps)
// an *ErrHTTP. Returns 0 and false otherwise.
func asHTTPStatus(err error) (int, bool) {
	if h, ok := errors.AsType[*ErrHTTP](err); ok {
		return h.Status, true
	}
	return 0, false
}

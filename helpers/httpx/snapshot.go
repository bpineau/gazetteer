package httpx

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// snapshotTransport is the outermost layer: it forwards the request,
// captures the response (drains + duplicates the body), and writes a
// JSON envelope to disk. Its presence is keyed off either Options.SnapshotDir
// or per-request WithSnapshot(ctx, dir). When neither is set, RoundTrip
// is a pass-through (no body buffering).
type snapshotTransport struct {
	next     http.RoundTripper
	resolved resolved
}

func newSnapshotTransport(next http.RoundTripper, r resolved) *snapshotTransport {
	return &snapshotTransport{next: next, resolved: r}
}

// snapshotEnvelope is the on-disk representation of a captured request/response.
type snapshotEnvelope struct {
	Request struct {
		Method  string      `json:"method"`
		URL     string      `json:"url"`
		Headers http.Header `json:"headers"`
	} `json:"request"`
	Response struct {
		Status      int         `json:"status"`
		Headers     http.Header `json:"headers"`
		DurationMs  int64       `json:"duration_ms"`
		FromCache   bool        `json:"from_cache"`
		Attempts    int         `json:"attempts"` // upstream sends, see Response.Attempts
		ContentType string      `json:"content_type,omitempty"`
	} `json:"response"`
	Body         string `json:"body"`
	BodyEncoding string `json:"body_encoding"` // "utf-8" or "base64"
	// BodyTruncated is true when the response was longer than
	// Options.MaxResponseBytes: Body then holds its first MaxResponseBytes
	// bytes only (the caller still receives the stream, and GetBytes
	// rejects it as oversized).
	BodyTruncated bool   `json:"body_truncated,omitempty"`
	CapturedAt    string `json:"captured_at"`
}

// snapshotFileMode and snapshotDirMode keep snapshots private to their
// owner: even redacted, an envelope records the full exchange with an
// upstream (URLs, cookies set by the server's pages, response bodies).
const (
	snapshotFileMode os.FileMode = 0o600
	snapshotDirMode  os.FileMode = 0o700
)

// redactedHeaders are the header names whose values a snapshot never
// records, in requests or responses alike.
var redactedHeaders = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	"Cookie":              true,
	"Set-Cookie":          true,
	"X-Api-Key":           true,
	"Api-Key":             true,
	"X-Auth-Token":        true,
	"X-Csrf-Token":        true,
}

// redactedQueryParams are the URL query parameters a snapshot masks: the
// usual names an API key travels under.
var redactedQueryParams = []string{"key", "apikey", "api_key", "token", "access_token", "secret", "password"}

// redactHeader returns a copy of h whose credential-bearing values are
// replaced by "[redacted]" (the header names are kept, so a snapshot still
// shows that a credential was sent).
func redactHeader(h http.Header) http.Header {
	out := h.Clone()
	for k := range out {
		ck := http.CanonicalHeaderKey(k)
		lk := strings.ToLower(ck)
		if redactedHeaders[ck] || strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "api-key") {
			for i := range out[k] {
				out[k][i] = "[redacted]"
			}
		}
	}
	return out
}

// redactURL renders u without its userinfo password and with the values
// of credential-like query parameters masked.
func redactURL(u *url.URL) string {
	c := *u
	if q := c.Query(); len(q) > 0 {
		changed := false
		for k := range q {
			if slices.Contains(redactedQueryParams, strings.ToLower(k)) {
				q.Set(k, "redacted")
				changed = true
			}
		}
		if changed {
			c.RawQuery = q.Encode()
		}
	}
	return c.Redacted()
}

// RoundTrip implements http.RoundTripper.
func (t *snapshotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dir := t.activeDir(req.Context())
	if dir == "" {
		// No snapshot configured: pass-through.
		return t.next.RoundTrip(req)
	}

	start := t.resolved.now()
	ctx, attempts := withAttemptCounter(req.Context())
	req = req.WithContext(ctx)
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	// Capture at most MaxResponseBytes (+1 to detect the overrun): the
	// snapshot must not lift the client's response-size bound. The caller
	// gets the captured prefix followed by the rest of the live stream, so
	// GetBytes and Download still enforce their own limits on it.
	var src io.Reader = resp.Body
	limit := t.resolved.maxResponseBytes
	if limit > 0 {
		src = io.LimitReader(resp.Body, limit+1)
	}
	body, rerr := io.ReadAll(src)
	if rerr != nil {
		_ = resp.Body.Close()
		return nil, rerr
	}
	truncated := limit > 0 && int64(len(body)) > limit
	resp.Body = readCloser{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}
	if truncated {
		body = body[:limit]
	}

	// Best-effort write; never fail the request because of a snapshot.
	if werr := t.writeSnapshot(req, resp, body, truncated, start, attemptsOf(attempts), dir); werr != nil {
		t.resolved.logger.Warn("snapshot write failed",
			"url", req.URL.String(),
			"err", werr.Error(),
		)
	}
	return resp, nil
}

// activeDir resolves the directory to use, preferring per-request override.
func (t *snapshotTransport) activeDir(ctx interface{ Value(any) any }) string {
	if d, ok := ctxValue(ctx, ctxKeySnapshotDir).(string); ok && d != "" {
		return d
	}
	return t.resolved.snapshotDir
}

// ctxValue is a tiny helper to indirect through any context-like.
func ctxValue(c interface{ Value(any) any }, k any) any { return c.Value(k) }

// readCloser pairs a reader with the Close of the stream it drains.
type readCloser struct {
	io.Reader
	io.Closer
}

func (t *snapshotTransport) writeSnapshot(req *http.Request, resp *http.Response, body []byte, truncated bool, start time.Time, attempts int, baseDir string) error {
	src := SourceFromContext(req.Context())
	runID := RunIDFromContext(req.Context())
	if src == "" {
		src = "_"
	}
	if runID == "" {
		runID = "_"
	}
	day := start.UTC().Format("2006-01-02")
	hash := requestHash(req)
	ext := guessExt(resp.Header.Get("Content-Type"))

	dir := filepath.Join(baseDir, src, day, runID)
	if err := os.MkdirAll(dir, snapshotDirMode); err != nil {
		return err
	}
	path := filepath.Join(dir, hash+"."+ext)

	env := snapshotEnvelope{}
	env.Request.Method = req.Method
	env.Request.URL = redactURL(req.URL)
	env.Request.Headers = redactHeader(req.Header)
	env.Response.Status = resp.StatusCode
	env.Response.Headers = redactHeader(resp.Header)
	env.Response.DurationMs = t.resolved.now().Sub(start).Milliseconds()
	env.Response.FromCache = resp.Header.Get("X-From-Cache") == "1"
	env.Response.Attempts = attempts
	env.BodyTruncated = truncated
	env.Response.ContentType = resp.Header.Get("Content-Type")
	env.CapturedAt = start.UTC().Format(time.RFC3339)

	if isTextual(env.Response.ContentType) && utf8.Valid(body) {
		env.Body = string(body)
		env.BodyEncoding = "utf-8"
	} else {
		env.Body = base64.StdEncoding.EncodeToString(body)
		env.BodyEncoding = "base64"
	}

	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out, snapshotFileMode)
}

func guessExt(ct string) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "html"):
		return "html"
	case strings.Contains(ct, "xml"):
		return "xml"
	case strings.HasPrefix(ct, "text/"):
		return "txt"
	case strings.HasPrefix(ct, "image/"):
		// "image/png" -> "png"
		parts := strings.SplitN(ct, "/", 2)
		if len(parts) == 2 {
			ct2 := strings.SplitN(parts[1], ";", 2)[0]
			return strings.TrimSpace(ct2)
		}
		fallthrough
	default:
		return "bin"
	}
}

func isTextual(ct string) bool {
	ct = strings.ToLower(ct)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	if strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "javascript") {
		return true
	}
	return false
}

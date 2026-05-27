package httpx

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		Attempts    int         `json:"attempts"` // 1 = single shot; populated by client
		ContentType string      `json:"content_type,omitempty"`
	} `json:"response"`
	Body         string `json:"body"`
	BodyEncoding string `json:"body_encoding"` // "utf-8" or "base64"
	CapturedAt   string `json:"captured_at"`
}

// RoundTrip implements http.RoundTripper.
func (t *snapshotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dir := t.activeDir(req.Context())
	if dir == "" {
		// No snapshot configured: pass-through.
		return t.next.RoundTrip(req)
	}

	start := t.resolved.now()
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	body, rerr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if rerr != nil {
		return nil, rerr
	}
	// Re-attach the body for the caller.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	// Best-effort write; never fail the request because of a snapshot.
	if werr := t.writeSnapshot(req, resp, body, start, dir); werr != nil {
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

func (t *snapshotTransport) writeSnapshot(req *http.Request, resp *http.Response, body []byte, start time.Time, baseDir string) error {
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
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // snapshot archive dir; no secrets
		return err
	}
	path := filepath.Join(dir, hash+"."+ext)

	env := snapshotEnvelope{}
	env.Request.Method = req.Method
	env.Request.URL = req.URL.String()
	env.Request.Headers = req.Header.Clone()
	env.Response.Status = resp.StatusCode
	env.Response.Headers = resp.Header.Clone()
	env.Response.DurationMs = time.Since(start).Milliseconds()
	env.Response.FromCache = resp.Header.Get("X-From-Cache") == "1"
	env.Response.Attempts = 1
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
	return writeFileAtomic(path, out, 0o644)
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

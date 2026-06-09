package httpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cacheTransport implements an on-disk persistent HTTP cache.
//
// Layout: each entry produces 2 files under <dir>/<2chars>/<hash>.{json,body}.
// Two files (meta + body) instead of one binary file: easier to inspect,
// trivial to scan/repair. Trade-off accepted in spec §"Format".
//
// Logic:
//   - Cacheable methods : GET, HEAD.
//   - Cacheable statuses: 200, 203, 300, 301, 308.
//   - On a fresh hit (now < expires_at): served straight from disk, never
//     forwarded.
//   - On a stale hit with validators: forward with If-None-Match /
//     If-Modified-Since. 304 → bump fetched_at and serve cached body. Other
//     → store new entry.
//   - On a stale hit without validators: forward; replace.
//   - On miss: forward; store if cacheable.
type cacheTransport struct {
	next     http.RoundTripper
	resolved resolved
	dir      string
}

func newCacheTransport(next http.RoundTripper, r resolved, dir string) *cacheTransport {
	return &cacheTransport{next: next, resolved: r, dir: dir}
}

// cacheMeta is the on-disk JSON metadata for a cache entry.
type cacheMeta struct {
	URL          string      `json:"url"`
	Method       string      `json:"method"`
	Status       int         `json:"status"`
	Header       http.Header `json:"header"`
	FetchedAtSec int64       `json:"fetched_at"`
	ExpiresAtSec int64       `json:"expires_at,omitempty"` // 0 = never auto-expires (rely on validators)
	BodyLen      int64       `json:"body_len"`
}

// RoundTrip implements http.RoundTripper.
func (t *cacheTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// Bypass: never read or write.
	if BypassCacheFromContext(ctx) {
		return t.next.RoundTrip(req)
	}
	// Only GET/HEAD are cacheable.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return t.next.RoundTrip(req)
	}

	hash := requestHash(req)
	metaPath, bodyPath := t.pathsFor(hash)

	if meta, body, ok := t.readEntry(metaPath, bodyPath); ok {
		now := t.resolved.now()
		fresh := meta.ExpiresAtSec > 0 && now.Unix() < meta.ExpiresAtSec
		if fresh {
			return buildResponse(req, meta, body, true), nil
		}
		// Stale but with validators? Try conditional GET.
		etag := meta.Header.Get("ETag")
		lastMod := meta.Header.Get("Last-Modified")
		if etag != "" || lastMod != "" {
			condReq := req.Clone(ctx)
			if condReq.Header == nil {
				condReq.Header = make(http.Header)
			}
			if etag != "" {
				condReq.Header.Set("If-None-Match", etag)
			}
			if lastMod != "" {
				condReq.Header.Set("If-Modified-Since", lastMod)
			}
			resp, err := t.next.RoundTrip(condReq)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode == http.StatusNotModified {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				// Refresh fetched_at + expires_at if Cache-Control said so.
				meta.FetchedAtSec = now.Unix()
				meta.ExpiresAtSec = computeExpiry(resp.Header, now, t.resolved.defaultTTL)
				_ = t.writeMeta(metaPath, meta)
				return buildResponse(req, meta, body, true), nil
			}
			// Otherwise the server gave a fresh response; persist it.
			return t.persistAndReturn(req, resp, metaPath, bodyPath)
		}
		// Stale without validators: refetch normally.
	}

	// Miss: forward and (maybe) cache.
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	return t.persistAndReturn(req, resp, metaPath, bodyPath)
}

// persistAndReturn drains the response body, writes it to disk if the
// status is cacheable, and returns a fresh *http.Response with the body
// re-attached (so the caller can read it once more).
func (t *cacheTransport) persistAndReturn(req *http.Request, resp *http.Response, metaPath, bodyPath string) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// Build a re-readable response in any case.
	out := &http.Response{
		Status:        resp.Status,
		StatusCode:    resp.StatusCode,
		Proto:         resp.Proto,
		ProtoMajor:    resp.ProtoMajor,
		ProtoMinor:    resp.ProtoMinor,
		Header:        resp.Header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}

	if !isStatusCacheable(resp.StatusCode) {
		return out, nil
	}

	now := t.resolved.now()
	meta := &cacheMeta{
		URL:          req.URL.String(),
		Method:       req.Method,
		Status:       resp.StatusCode,
		Header:       canonicalizeHeader(resp.Header),
		FetchedAtSec: now.Unix(),
		ExpiresAtSec: computeExpiry(resp.Header, now, t.resolved.defaultTTL),
		BodyLen:      int64(len(body)),
	}
	if err := t.writeEntry(metaPath, bodyPath, meta, body); err != nil {
		t.resolved.logger.Warn("cache write failed",
			"url", req.URL.String(),
			"err", err.Error(),
		)
	}
	return out, nil
}

// pathsFor returns the meta and body paths for a given hash.
func (t *cacheTransport) pathsFor(hash string) (meta, body string) {
	prefix := hash[:2]
	dir := filepath.Join(t.dir, prefix)
	return filepath.Join(dir, hash+".json"), filepath.Join(dir, hash+".body")
}

// readEntry loads a cache entry. Returns (nil,nil,false) on any error or
// missing file — callers treat that as a cache miss.
func (t *cacheTransport) readEntry(metaPath, bodyPath string) (*cacheMeta, []byte, bool) {
	mb, err := os.ReadFile(metaPath) //nolint:gosec // metaPath/bodyPath derived from a SHA-256 hash of the request; not user-supplied
	if err != nil {
		return nil, nil, false
	}
	var meta cacheMeta
	if err := json.Unmarshal(mb, &meta); err != nil {
		return nil, nil, false
	}
	meta.Header = canonicalizeHeader(meta.Header)
	body, err := os.ReadFile(bodyPath) //nolint:gosec // see above
	if err != nil {
		return nil, nil, false
	}
	return &meta, body, true
}

func (t *cacheTransport) writeEntry(metaPath, bodyPath string, meta *cacheMeta, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil { //nolint:gosec // public HTTP cache dir; not a secrets store
		return err
	}
	if err := writeFileAtomic(bodyPath, body, 0o644); err != nil {
		return err
	}
	if err := t.writeMeta(metaPath, meta); err != nil {
		return err
	}
	return nil
}

func (t *cacheTransport) writeMeta(metaPath string, meta *cacheMeta) error {
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil { //nolint:gosec // public HTTP cache dir; not a secrets store
		return err
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(metaPath, mb, 0o644)
}

// writeFileAtomic writes data to path via a sibling .tmp + rename.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) //nolint:gosec // tmp = path+".tmp"; path is derived from a SHA-256 cache key, not user input
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// buildResponse rebuilds an *http.Response from on-disk cache data.
func buildResponse(req *http.Request, meta *cacheMeta, body []byte, fromCache bool) *http.Response {
	hdr := meta.Header.Clone()
	if fromCache {
		hdr.Set("X-From-Cache", "1")
	}
	return &http.Response{
		Status:        strconv.Itoa(meta.Status) + " " + http.StatusText(meta.Status),
		StatusCode:    meta.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        hdr,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

// requestHash returns the cache key for a request.
//
// Key = sha256(method "\n" url). v1 ignores Vary when hashing the
// first request and relies on the response's Vary header to
// disambiguate later — a varyKey parameter is intentionally absent
// so the call sites stay simple. Extend the signature when v2 wires
// proactive Vary-derived hashing.
func requestHash(req *http.Request) string {
	h := sha256.New()
	_, _ = io.WriteString(h, req.Method)
	_, _ = io.WriteString(h, "\n")
	_, _ = io.WriteString(h, req.URL.String())
	return hex.EncodeToString(h.Sum(nil))
}

// isStatusCacheable per spec §"Politique".
func isStatusCacheable(status int) bool {
	switch status {
	case 200, 203, 300, 301, 308:
		return true
	}
	return false
}

// computeExpiry derives the expiry instant from Cache-Control: max-age
// (preferred) or Expires header. Returns 0 (= "no auto-expiry; rely on
// validators") when none is present and no defaultTTL is set; otherwise
// falls back to defaultTTL.
func computeExpiry(h http.Header, now time.Time, defaultTTL time.Duration) int64 {
	if cc := h.Get("Cache-Control"); cc != "" {
		// Look for max-age=N and no-store.
		parts := strings.SplitSeq(cc, ",")
		for p := range parts {
			p = strings.TrimSpace(strings.ToLower(p))
			if p == "no-store" || p == "no-cache" {
				return 0 // forces revalidation every time
			}
			if after, ok := strings.CutPrefix(p, "max-age="); ok {
				if n, err := strconv.Atoi(after); err == nil && n >= 0 {
					return now.Add(time.Duration(n) * time.Second).Unix()
				}
			}
		}
	}
	if exp := h.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			return t.Unix()
		}
	}
	if defaultTTL > 0 {
		return now.Add(defaultTTL).Unix()
	}
	return 0
}

// canonicalizeHeader returns a copy of h whose keys all use the canonical
// MIME header form (e.g. "ETag" → "Etag"). The Go HTTP server canonicalises
// on the wire, but tests/fixtures that build http.Header directly may not;
// we normalise on cache-write so Header.Get works regardless.
func canonicalizeHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vs := range h {
		// http.Header.Set/Add canonicalise their key argument internally.
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	return out
}

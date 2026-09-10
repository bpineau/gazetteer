package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
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
//
// Everything that is not a fresh hit goes through ONE upstream trip per
// cache key, shared by every caller waiting on it (see fetchShared): a cold
// cache and a burst of identical requests cost one request, not one each.
//
// Nothing here prunes: entries live until an operator calls
// Client.PruneCache. See its godoc for why that decision belongs to the
// caller.
type cacheTransport struct {
	next     http.RoundTripper
	resolved resolved
	dir      string

	// flights coalesces the concurrent misses and revalidations of one
	// cache key into a single upstream trip. It merges in-flight
	// duplicates only: a completed trip releases its key, and the next
	// request reads the entry it wrote. See fetchShared.
	flights singleflight.Group
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

	// Fresh hit: straight off the disk, no flight, no upstream.
	if meta, body, ok := t.readEntry(metaPath, bodyPath); ok {
		if now := t.resolved.now(); meta.ExpiresAtSec > 0 && now.Unix() < meta.ExpiresAtSec {
			return fetchedFromEntry(meta, body).response(req), nil
		}
	}

	// Miss or stale: one upstream trip per key, shared by every caller.
	f, err := t.fetchShared(req, hash, metaPath, bodyPath)
	if err != nil {
		return nil, err
	}
	return f.response(req), nil
}

// sharedFetchTimeout bounds a coalesced upstream trip when the initiating
// request carries no deadline of its own. The trip is detached from that
// caller's CANCELLATION (see fetchShared), so it needs a ceiling of its own
// or a hung server would pin the goroutine, and the body it is buffering,
// for the life of the process. It matches the client-wide request timeout.
const sharedFetchTimeout = defaultClientTimeout

// fetchShared performs the upstream trip for a miss or a stale entry, with
// concurrent callers for the same cache key coalesced into ONE trip. It is
// the disk cache's stampede guard: a cold entry hit by a burst of identical
// requests (the common shape here, several Sources geocoding the same
// address at once) costs one upstream request instead of one per caller, and
// one write instead of a pile of racing writes for the same entry.
//
// Three properties are load-bearing:
//
//   - Context isolation. The shared trip runs on the initiator's context
//     VALUES with its cancellation detached, so the caller that happens to
//     start the trip cannot cancel it out from under the callers that joined
//     it. It stays bounded: the initiator's own deadline when it has one,
//     else sharedFetchTimeout. Each caller selects on its OWN context, so a
//     cancelled caller returns immediately with its context error.
//
//   - Error fidelity. singleflight hands the (value, error) pair to every
//     waiter, so a transport failure surfaces AS an error to all of them.
//     Nothing is written on failure, and a non-cacheable status (a 500, a
//     404) is passed through to every waiter WITHOUT being cached: an error
//     is never banked as an empty success.
//
//   - Body independence. The trip materialises the body in memory and each
//     caller gets its own reader over those bytes, so N waiters read one
//     buffer instead of racing over one socket.
//
// The coalescing key is the cache key, which is method+URL (requestHash):
// two requests that differ only by header therefore share one response, the
// same way they already shared one cache entry.
func (t *cacheTransport) fetchShared(req *http.Request, key, metaPath, bodyPath string) (*fetched, error) {
	ctx := req.Context()
	ch := t.flights.DoChan(key, func() (any, error) {
		shared, cancel := sharedRequestContext(ctx, sharedFetchTimeout)
		defer cancel()
		return t.forward(req.Clone(shared), metaPath, bodyPath)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*fetched), nil
	}
}

// sharedRequestContext derives the context a coalesced trip runs on: the
// initiating request's VALUES with its cancellation detached, bounded by its
// own deadline when it has one and by fallback otherwise.
func sharedRequestContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, fallback)
}

// forward performs the one upstream trip behind a flight: a conditional GET
// when the stale entry carries validators (a 304 refreshes the entry's
// timestamps and serves the cached body), a plain GET otherwise. The entry
// is re-read here, inside the flight, so the validators come from whatever
// is on disk at the moment the trip is made.
func (t *cacheTransport) forward(req *http.Request, metaPath, bodyPath string) (*fetched, error) {
	if meta, body, ok := t.readEntry(metaPath, bodyPath); ok {
		etag := meta.Header.Get("ETag")
		lastMod := meta.Header.Get("Last-Modified")
		if etag != "" || lastMod != "" {
			condReq := req.Clone(req.Context())
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
				now := t.resolved.now()
				meta.FetchedAtSec = now.Unix()
				meta.ExpiresAtSec = computeExpiry(resp.Header, now, t.resolved.defaultTTL)
				_ = t.writeMeta(metaPath, meta)
				return fetchedFromEntry(meta, body), nil
			}
			// Otherwise the server gave a fresh response; persist it.
			return t.persist(req, resp, metaPath, bodyPath)
		}
		// Stale without validators: refetch normally.
	}

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	return t.persist(req, resp, metaPath, bodyPath)
}

// fetched is one completed trip, materialised: the status line, headers and
// full body, from which every caller sharing the trip builds its own
// *http.Response.
type fetched struct {
	// status is the upstream status line ("200 OK"), rebuilt from the
	// status code for an entry served off disk.
	status string
	// statusCode is the HTTP status.
	statusCode int
	// proto / protoMajor / protoMinor are the response's protocol, as
	// reported by the upstream ("HTTP/1.1" for a disk entry).
	proto      string
	protoMajor int
	protoMinor int
	// header is the response header set, canonicalised.
	header http.Header
	// body is the whole response body, already read.
	body []byte
	// fromCache marks a body that came off the disk (a fresh hit, or a
	// revalidation the upstream answered with 304), so response() can stamp
	// X-From-Cache.
	fromCache bool
}

// response builds an independent *http.Response for one caller: its own body
// reader over the shared bytes, its own header copy, its own Request.
func (f *fetched) response(req *http.Request) *http.Response {
	hdr := f.header.Clone()
	if f.fromCache {
		hdr.Set("X-From-Cache", "1")
	}
	return &http.Response{
		Status:        f.status,
		StatusCode:    f.statusCode,
		Proto:         f.proto,
		ProtoMajor:    f.protoMajor,
		ProtoMinor:    f.protoMinor,
		Header:        hdr,
		Body:          io.NopCloser(bytes.NewReader(f.body)),
		ContentLength: int64(len(f.body)),
		Request:       req,
	}
}

// fetchedFromEntry rebuilds a fetched from on-disk cache data.
func fetchedFromEntry(meta *cacheMeta, body []byte) *fetched {
	return &fetched{
		status:     strconv.Itoa(meta.Status) + " " + http.StatusText(meta.Status),
		statusCode: meta.Status,
		proto:      "HTTP/1.1",
		protoMajor: 1,
		protoMinor: 1,
		header:     meta.Header,
		body:       body,
		fromCache:  true,
	}
}

// persist drains the response body, writes it to disk if the status is
// cacheable, and returns the materialised trip (so every caller sharing it
// can read the body once more).
func (t *cacheTransport) persist(req *http.Request, resp *http.Response, metaPath, bodyPath string) (*fetched, error) {
	// Bound the read: this buffer sits BELOW GetBytes' io.LimitReader and
	// below Download's streaming io.Copy, so without a limit of its own an
	// oversized (or hostile) response is fully resident in RAM before either
	// guard ever sees it. Read one byte past the limit so the overrun is
	// detectable, and fail rather than cache a truncated body.
	var src io.Reader = resp.Body
	limit := t.resolved.maxResponseBytes
	if limit > 0 {
		src = io.LimitReader(resp.Body, limit+1)
	}
	body, err := io.ReadAll(src)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, fmt.Errorf("httpx: response from %s exceeds MaxResponseBytes=%d", req.URL, limit)
	}

	// Materialise the trip in any case, cacheable or not.
	out := &fetched{
		status:     resp.Status,
		statusCode: resp.StatusCode,
		proto:      resp.Proto,
		protoMajor: resp.ProtoMajor,
		protoMinor: resp.ProtoMinor,
		header:     canonicalizeHeader(resp.Header),
		body:       body,
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
	// Integrity guard: a body whose length disagrees with the meta record is a
	// torn or truncated write (e.g. crash between the atomic body write and the
	// meta write, or external tampering). Treat it as a miss so a fresh fetch
	// replaces it rather than serving a partial response.
	if int64(len(body)) != meta.BodyLen {
		return nil, nil, false
	}
	return &meta, body, true
}

func (t *cacheTransport) writeEntry(metaPath, bodyPath string, meta *cacheMeta, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil { //nolint:gosec // public HTTP cache dir; not a secrets store
		return err
	}
	if err := writeFileAtomic(bodyPath, body); err != nil {
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
	return writeFileAtomic(metaPath, mb)
}

// cacheFileMode is the mode of every on-disk cache file: world-readable, like
// the cache directory itself (a public HTTP cache, not a secrets store).
const cacheFileMode os.FileMode = 0o644

// writeFileAtomic writes data to path via a sibling tmpfile + rename, the
// tmpfile unique to this writer.
//
// Uniqueness is load-bearing: fetchShared now coalesces the concurrent
// misses of ONE key, but two writers for the same entry are still reachable
// (a caller that bypasses the cache layer for the read but not the write, two
// processes sharing a cache directory, a prune racing a write). With a fixed
// "<path>.tmp" such writers opened it O_TRUNC, interleaved their bytes and
// both renamed the mixture into place. readEntry's BodyLen check caught most
// torn bodies; the meta file had no such guard.
//
// Deliberately not helpers/atomicfs.WriteFile: the HTTP cache fsyncs
// before the rename so a crash can't leave a renamed-but-empty cache
// entry that would later be served as a valid response. atomicfs serves
// callers whose artifacts are re-derivable and skips the fsync cost.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp") //nolint:gosec // path is derived from a SHA-256 cache key, not user input
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := f.Chmod(cacheFileMode); err != nil { // os.CreateTemp always creates 0600
		_ = f.Close()
		_ = os.Remove(tmp)
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

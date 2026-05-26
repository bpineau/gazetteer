package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

// imageExtForMIME maps image MIME types to canonical file extensions.
// Only image types are handled here; non-image types (PDF, etc.) are
// left to the caller's pre-determined extension. Returns "" when the
// type is unknown or not an image.
func imageExtForMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	default:
		return ""
	}
}

// correctImageExt returns the extension that matches the actual content
// of an image file, sniffing the first 512 bytes. If the content is not
// a recognised image type, or if the sniffed type matches the current
// extension already, the current extension is returned unchanged (no-op).
func correctImageExt(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is the file we just wrote; trusted callsite
	if err != nil {
		return filepath.Ext(path), err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && n == 0 {
		return filepath.Ext(path), nil
	}
	sniffed := http.DetectContentType(buf[:n])
	// Strip parameters (e.g. "image/png; charset=utf-8" → "image/png").
	mt, _, _ := mime.ParseMediaType(sniffed)
	ext := imageExtForMIME(mt)
	if ext == "" {
		return filepath.Ext(path), nil
	}
	return ext, nil
}

// Download fetches url and writes it to destPath atomically. The body is
// streamed to a sibling .tmp file while sha256 is computed, then renamed
// into place. On any error, the .tmp is removed.
//
// The opts.SkipIfExists shortcut never touches the network; it instead
// hashes the file already on disk and returns it as the result. This is
// the canonical fast-path used by the document downloader to avoid
// re-fetching media we already have.
func (c *Client) Download(ctx context.Context, url, destPath string, opts DownloadOptions) (DownloadResult, error) {
	// Fast path: already there.
	if opts.SkipIfExists {
		if st, err := os.Stat(destPath); err == nil && !st.IsDir() {
			sum, n, err := sha256OfFile(destPath)
			if err != nil {
				return DownloadResult{}, fmt.Errorf("httpx: rehash existing %s: %w", destPath, err)
			}
			if opts.ExpectedSHA256 != "" && sum != opts.ExpectedSHA256 {
				return DownloadResult{}, fmt.Errorf("httpx: existing file at %s has sha256=%s, expected %s", destPath, sum, opts.ExpectedSHA256)
			}
			return DownloadResult{
				SHA256:  sum,
				Bytes:   n,
				Cached:  false,
				Skipped: true,
				Path:    destPath,
			}, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil { //nolint:gosec // public download dir; not a secrets store
		return DownloadResult{}, fmt.Errorf("httpx: mkdir %s: %w", filepath.Dir(destPath), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("httpx: build request for %s: %w", url, err)
	}
	c.applyDefaultHeaders(req, nil)

	resp, err := c.http.Do(req)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return DownloadResult{}, &ErrHTTP{Status: resp.StatusCode, URL: url}
	}
	fromCache := resp.Header.Get("X-From-Cache") == "1"

	tmp := destPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // public artefact dir; 0o644 lets read-only users (analysts) inspect downloaded PDFs/images
	if err != nil {
		return DownloadResult{}, fmt.Errorf("httpx: open tmp %s: %w", tmp, err)
	}

	limit := opts.MaxBytes
	if limit == 0 {
		limit = c.resolved.maxResponseBytes
	}

	hasher := sha256.New()
	mw := io.MultiWriter(f, hasher)

	var src io.Reader = resp.Body
	if limit > 0 {
		src = io.LimitReader(resp.Body, limit+1)
	}

	n, err := io.Copy(mw, src)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: write %s: %w", tmp, err)
	}
	if limit > 0 && n > limit {
		_ = f.Close()
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: download from %s exceeds MaxBytes=%d", url, limit)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: close %s: %w", tmp, err)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if opts.ExpectedSHA256 != "" && sum != opts.ExpectedSHA256 {
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: sha256 mismatch for %s: got %s, want %s", url, sum, opts.ExpectedSHA256)
	}

	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("httpx: rename %s -> %s: %w", tmp, destPath, err)
	}

	// For image files: sniff the first bytes to confirm the extension
	// matches the actual content type. Servers (vench, avoventes) sometimes
	// serve PNG/WebP bytes at a URL whose path ends in ".jpg". When the
	// sniffed extension differs, rename the file so the on-disk path and
	// the returned Path always carry the correct extension. The caller
	// (pipeline ingest) stores Path as documents.local_path, so the DB
	// stays consistent with the file at rest.
	//
	// Only image types are corrected; PDFs and other binary types are left
	// as-is — http.DetectContentType is unreliable for PDF (first bytes may
	// be a BOM or a comment and the detector falls back to text/plain).
	finalPath := destPath
	if currentExt := filepath.Ext(destPath); currentExt == ".jpg" || currentExt == ".jpeg" ||
		currentExt == ".png" || currentExt == ".webp" || currentExt == ".gif" || currentExt == ".bmp" {
		sniffedExt, _ := correctImageExt(destPath)
		if sniffedExt != "" && sniffedExt != currentExt {
			corrected := destPath[:len(destPath)-len(currentExt)] + sniffedExt
			if err := os.Rename(destPath, corrected); err == nil {
				finalPath = corrected
			}
			// On rename error: keep the original path; content is still
			// served — the browser will mis-decode it but it's not lost.
		}
	}

	return DownloadResult{
		SHA256: sum,
		Bytes:  n,
		Cached: fromCache,
		Path:   finalPath,
	}, nil
}

// sha256OfFile returns the sha256 hex and byte count of path.
func sha256OfFile(path string) (string, int64, error) {
	f, err := os.Open(path) //nolint:gosec // path is the file we just wrote; trusted callsite
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

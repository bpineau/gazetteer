# scrape — fetch + parse + decode in 5 lines

A thin assembly on top of `helpers/httpx` and goquery. The default path
is a fully-cached, rate-limited, snapshot-tapped HTML walker; the bricks
(`ParseHTML`, `AbsoluteURL`, `Doer`) are individually exported and
swappable for sites that need custom plumbing.

## Why this package exists

Every HTML-shaped Source ends up wiring the same plumbing independently:

```go
body, _, err := http.GetBytes(ctx, base+path, nil)   // resolve URL, GET
if err != nil { return err }
doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))  // parse
if err != nil { return err }
// site-specific logic
```

Without consolidation, `AbsoluteURL` grows three slightly different
copies per adapter and the goquery error-wrapping convention varies.
`scrape` is the one place those three lines live; every adapter imports
it.

## Principles

- **Default assembly is opinionated.** `Walker.Walk` does the canonical
  thing: resolve the URL, GET via the supplied Doer, parse the body,
  hand the document + raw bytes to a callback. No flags, no struct of
  options. Five lines of caller code.
- **Bricks compose; bricks are replaceable.** `ParseHTML` and
  `AbsoluteURL` are exported; `Doer` is a one-method interface; the
  HandlerFunc receives both the parsed doc AND the raw bytes (for
  inline-JSON extractions goquery normalises away). Any of these can
  be used in isolation — they don't depend on each other.
- **No site-specific logic.** The package knows nothing about
  Cloudflare, DataDome, French postcodes or pagination. Anti-bot
  detection lives in the sibling `helpers/scrape/antibot` package;
  paginated cursors are an adapter concern.
- **Errors wrap, never replace.** `Walker.Walk` returns the callback's
  error verbatim on success-of-the-pipeline; transport / parse errors
  are wrapped with `scrape:` prefix so callers can keep adding their
  own context.

## Quick start

```go
import (
    "context"
    "github.com/bpineau/gazetteer/helpers/httpx"
    "github.com/bpineau/gazetteer/helpers/scrape"
    "github.com/PuerkitoBio/goquery"
)

http, _ := httpx.New(httpx.Options{HTTPCacheDir: "data/cache/http"})
walker := scrape.NewWalker(http, "https://www.example.fr")

err := walker.Walk(ctx, "/listing-1.html", nil, func(_ context.Context, doc *goquery.Document, _ []byte) error {
    title := doc.Find("h1").First().Text()
    fmt.Println(title)
    return nil
})
```

That's it. Five lines plus the imports. The walker is fully cached,
rate-limited (per the httpx options), snapshot-tapped if you set
`SnapshotDir`, and retries on 5xx.

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/scrape` for the
godoc-rendered surface:

- `func ParseHTML([]byte) (*goquery.Document, error)`
- `func AbsoluteURL(base, ref string) string`
- `type Doer interface { GetBytes(ctx, url, hdr) ([]byte, *httpx.Response, error) }`
- `type HandlerFunc func(ctx, *goquery.Document, []byte) error`
- `type Walker struct { … }`
- `func NewWalker(Doer, base string) *Walker`
- `(*Walker) AbsoluteURL(path string) string`
- `(*Walker) GetRaw(ctx, path, hdr) ([]byte, error)`
- `(*Walker) Get(ctx, path, hdr) (*goquery.Document, []byte, error)`
- `(*Walker) Walk(ctx, path, hdr, HandlerFunc) error`

## Before / after

A typical "fetch a listing page, extract a list of detail-URLs" adapter
without `scrape` is ~25 LOC of plumbing per site:

```go
type Client struct { http *httpx.Client }
func NewClient(h *httpx.Client) *Client { return &Client{http: h} }
func (c *Client) GetListing(ctx context.Context) (*goquery.Document, []byte, error) {
    body, _, err := c.http.GetBytes(ctx, ListingURL, nil)
    if err != nil { return nil, nil, fmt.Errorf("GetListing: %w", err) }
    doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
    if err != nil { return nil, nil, fmt.Errorf("GetListing: parse: %w", err) }
    return doc, body, nil
}
func (c *Client) GetDetail(ctx context.Context, slug string) (*goquery.Document, []byte, error) {
    body, _, err := c.http.GetBytes(ctx, fmt.Sprintf(DetailFmt, slug), nil)
    // ... 10 more lines for parse + the not-found heuristic ...
}
```

After:

```go
walker := scrape.NewWalker(http, "https://www.example.fr")
walker.Walk(ctx, "/listing", nil, func(_ context.Context, doc *goquery.Document, _ []byte) error {
    doc.Find(".listing-item a").Each(func(_ int, s *goquery.Selection) {
        href, _ := s.Attr("href")
        slugs = append(slugs, href)
    })
    return nil
})
```

The "not-found heuristic" stays site-specific (it's a check on the body
shape) — that's what `Walker.Get` returning the raw bytes is for. The
generic plumbing drops from ~25 LOC per adapter to 0.

## When to reach down a layer

- **Anti-bot detection between GET and parse.** Use `Walker.GetRaw` to
  fetch the bytes, hand them to `helpers/scrape/antibot.Detector`,
  then call `scrape.ParseHTML(body)`.
- **Multi-base composition.** A walker is bound to one base URL. If
  your adapter spans `www.x.fr` AND `cdn.x.fr`, build two walkers (or
  call `httpx.GetBytes` directly for the off-base hops).
- **You need the `*http.Response` headers, not just bytes.** `Doer`
  intentionally returns only `[]byte` and the `httpx.Response` summary.
  When you need full response control, skip the Walker and call
  `httpx.GetBytes` (or `Client.HTTPClient().Do`) directly.

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.

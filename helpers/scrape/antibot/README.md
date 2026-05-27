# antibot — Cloudflare / DataDome / Captcha interstitial detection

One detector for HTML pages fronted by Cloudflare, DataDome, or a
generic "are you human" gate. Pure body / header inspection — no
network, no cookies, no challenge solving.

## Why this package exists

Without a shared detector, every scraper grows its own near-identical
marker list with subtly different false-positive guards. Drift is a
silent failure mode: a marker added to one parser but not the others
lets some sites cache a 24h skip while others happily parse an empty
interstitial.

## The load-bearing false-positive guard

Legitimate pages on sites that subscribe to DataDome embed the
DataDome client SDK at `js.datadome.co/tags.js` as a regular asset.
**A naive substring match on the bare token "datadome" would
systematically false-positive on every successful response** and turn
it into a 24h ErrAntiBot skip — silently killing the consumer.

Every marker in this package is chosen so that pages embedding tags.js
parse cleanly. `TestDataDome_LegitTagsJS_NotBlocked` pins the
contract; do not regress it.

## Public API

```go
package antibot

type Verdict int
const (
    None       Verdict = iota
    Cloudflare
    DataDome
    Captcha
)

type Detector interface {
    Detect(body []byte, headers http.Header) Verdict
}

func DefaultDetector() Detector  // composite of CF + DD + Captcha

// Each individual detector is exported so a special site can use just one.
type CloudflareDetector struct{}
type DataDomeDetector   struct{}
type CaptchaDetector    struct{}
```

`headers` may be nil — body-only callers pass nil. Cloudflare's
`cf-mitigated` response header is recognised when headers are present.

## Composite precedence

The composite runs Cloudflare → DataDome → Captcha and returns the
first non-`None` verdict. A page carrying both a CF title and a DD
script gets reported as Cloudflare. The standalone detectors continue
to flag their own regime if you need to distinguish.

## Adding a marker

Add it to the relevant detector's marker slice (`cfBodyMarkers`,
`ddBodyMarkers`, `captchaBodyMarkers`) with an inline comment naming
the site / run where it was first observed. Then add a positive test
case to `antibot_test.go`. If the marker risks false-positives on
clean pages, also add a negative case alongside
`TestDataDome_LegitTagsJS_NotBlocked`.

## Stability

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.

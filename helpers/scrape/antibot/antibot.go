// Package antibot consolidates Cloudflare / DataDome / Captcha
// interstitial detection into one place.
//
// The detection is heuristic substring + regex matching against the
// response body (and optionally the response headers). It exists
// because every adapter that touched a Cloudflare- or DataDome-fronted
// site had grown its own near-identical marker list with subtly
// different false-positive guards.
//
// # The load-bearing false-positive guard
//
// Legitimate MeilleursAgents pages embed the DataDome
// client SDK at `js.datadome.co/tags.js`. A naive substring match on
// the bare token "datadome" would systematically false-positive on
// every successful response and turn it into a 24h skip. Every marker
// in this package is chosen so that pages embedding tags.js as a
// regular asset parse cleanly. The TestDataDome_LegitTagsJS_NotBlocked
// test pins this contract.
//
// # Public API
//
//   - Verdict — the detector result. Verdict == None means clean.
//   - Detector interface — Detect(html, headers) Verdict.
//   - DefaultDetector() — composite of the three concrete detectors.
//   - CloudflareDetector{}, DataDomeDetector{}, CaptchaDetector{} —
//     individual detectors so a special site can pick just one.
//
// # Marker provenance
//
// Each marker is annotated inline with the source observation that
// motivated it. Adding a new marker is a one-line change with a
// pointer to the run where it was seen.
package antibot

import (
	"bytes"
	"net/http"
	"regexp"
)

// Verdict enumerates the antibot regimes this package can recognise.
//
//	None       — body looks like normal content.
//	Cloudflare — Cloudflare interstitial / "Just a moment".
//	DataDome   — DataDome block / captcha-delivery iframe.
//	Captcha    — generic captcha / "are you human" pattern that does
//	             not match a specific vendor.
//
// The order matters in the composite: more-specific vendors win over
// the generic Captcha verdict.
type Verdict int

// Verdict values.
const (
	None Verdict = iota
	Cloudflare
	DataDome
	Captcha
)

// String returns a stable short label for the verdict, suitable for
// log lines. Stable: callers may match on it.
func (v Verdict) String() string {
	switch v {
	case Cloudflare:
		return "cloudflare"
	case DataDome:
		return "datadome"
	case Captcha:
		return "captcha"
	default:
		return "none"
	}
}

// Detector reports whether a response looks like an antibot
// interstitial and, if so, which regime served it.
//
// headers may be nil — body-only callers use that path.
type Detector interface {
	Detect(body []byte, headers http.Header) Verdict
}

// CloudflareDetector flags Cloudflare's "Just a moment..." challenge
// page and the cf_chl / cdn-cgi/challenge-platform script tags it
// embeds. Markers gathered from vench.fr (REVERSE_ENG_PROCESS) and
// licitor web WAF responses.
type CloudflareDetector struct{}

// reCloudflareTitle matches "<title>Just a moment...</title>" with
// arbitrary whitespace, case-insensitive. From vench/selectors.go.
var reCloudflareTitle = regexp.MustCompile(`(?i)<title>\s*Just\s+a\s+moment`)

// reCloudflareCFCHL matches the cf_chl_opt JS object and the
// cdn-cgi/challenge-platform script path. From vench/selectors.go.
var reCloudflareCFCHL = regexp.MustCompile(`cf_chl_opt|cdn-cgi/challenge-platform`)

// cfBodyMarkers carry shorter substrings that appear on the
// interstitial body. Lowercase for case-insensitive substring scan.
var cfBodyMarkers = [][]byte{
	[]byte("cf-chl-bypass"),      // CF challenge marker (every parser)
	[]byte("cf_chl"),             // shorter CF challenge marker (licitorweb, castorus)
	[]byte("attention required"), // CF "Attention Required" page
}

// cfHeaderName is the response header CF stamps on a mitigation.
// Lowercase per net/http canonicalisation; we always check via
// http.Header.Get which canonicalises the lookup.
const cfHeaderName = "Cf-Mitigated"

// Detect reports a Cloudflare verdict when any title/script regex,
// body marker or response header is observed.
func (CloudflareDetector) Detect(body []byte, headers http.Header) Verdict {
	if headers != nil && headers.Get(cfHeaderName) != "" {
		return Cloudflare
	}
	if len(body) == 0 {
		return None
	}
	if reCloudflareTitle.Match(body) {
		return Cloudflare
	}
	if reCloudflareCFCHL.Match(body) {
		return Cloudflare
	}
	low := bytes.ToLower(body)
	for _, m := range cfBodyMarkers {
		if bytes.Contains(low, m) {
			return Cloudflare
		}
	}
	return None
}

// DataDomeDetector flags DataDome's challenge-delivery iframe and the
// `var dd=` global object DataDome injects on a block page.
//
// CRITICAL: this detector MUST NOT match the legitimate
// `js.datadome.co/tags.js` SDK reference embedded by every protected
// site on its normal pages. The marker list below appears only on
// real interstitials. See TestDataDome_LegitTagsJS_NotBlocked.
type DataDomeDetector struct{}

// ddBodyMarkers cover the DataDome interstitial without false-positing
// on tags.js. Provenance:
//   - "captcha-delivery.com" — the DataDome challenge iframe URL,
//     present only on a real block page.
//   - "var dd=" — the DataDome global config object emitted on the
//     block-page <script>.
//   - "_datadome_block" / "datadome-block" — block-page script id /
//     class, never on a clean page.
var ddBodyMarkers = [][]byte{
	[]byte("captcha-delivery.com"),
	[]byte("var dd="),
	[]byte("_datadome_block"),
	[]byte("datadome-block"),
}

// Detect reports a DataDome verdict when the body carries one of the
// block-page-only markers. Headers are unused: DataDome serves the
// challenge as a 200 OK with no specific stamp.
func (DataDomeDetector) Detect(body []byte, _ http.Header) Verdict {
	if len(body) == 0 {
		return None
	}
	low := bytes.ToLower(body)
	for _, m := range ddBodyMarkers {
		if bytes.Contains(low, m) {
			return DataDome
		}
	}
	return None
}

// CaptchaDetector flags generic "are you a robot" wording that does
// not specifically match Cloudflare or DataDome. Most often this is
// a custom WAF or a generic captcha bouncer.
type CaptchaDetector struct{}

var captchaBodyMarkers = [][]byte{
	[]byte("vous n'êtes pas un robot"),
	[]byte("you are not a robot"),
	[]byte("please verify you are human"),
	[]byte("please enable js and disable any ad blocker"),
}

// Detect reports a Captcha verdict on any of the generic markers.
func (CaptchaDetector) Detect(body []byte, _ http.Header) Verdict {
	if len(body) == 0 {
		return None
	}
	low := bytes.ToLower(body)
	for _, m := range captchaBodyMarkers {
		if bytes.Contains(low, m) {
			return Captcha
		}
	}
	return None
}

// composite chains a Cloudflare → DataDome → Captcha detection. The
// first non-None verdict wins.
type composite struct {
	cf  CloudflareDetector
	dd  DataDomeDetector
	cap CaptchaDetector
}

// Detect runs the three sub-detectors in order; first hit wins.
func (c composite) Detect(body []byte, headers http.Header) Verdict {
	if v := c.cf.Detect(body, headers); v != None {
		return v
	}
	if v := c.dd.Detect(body, headers); v != None {
		return v
	}
	if v := c.cap.Detect(body, headers); v != None {
		return v
	}
	return None
}

// DefaultDetector returns the composite Cloudflare + DataDome +
// Captcha detector. Stable across releases — adding markers happens
// inside the constituent detectors.
func DefaultDetector() Detector {
	return composite{}
}

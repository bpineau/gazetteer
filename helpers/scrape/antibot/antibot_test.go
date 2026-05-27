package antibot

import (
	"net/http"
	"testing"
)

func TestDefaultDetector_Cloudflare(t *testing.T) {
	cases := map[string][]byte{
		"just-a-moment-title": []byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head>` +
			`<body><div id="cf-chl-bypass">checking your browser</div></body></html>`),
		"cf-chl-script": []byte(`<html><body>` +
			`<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script>` +
			`</body></html>`),
		"cf-chl-opt-object": []byte(`<html><body><script>window.cf_chl_opt={cType:"managed"};</script></body></html>`),
	}
	d := DefaultDetector()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := d.Detect(body, nil); got != Cloudflare {
				t.Errorf("verdict = %v, want Cloudflare", got)
			}
		})
	}
}

func TestDefaultDetector_CloudflareHeader(t *testing.T) {
	// cf-mitigated stamp on a body that is otherwise empty.
	h := http.Header{}
	h.Set("cf-mitigated", "challenge")
	if got := DefaultDetector().Detect(nil, h); got != Cloudflare {
		t.Errorf("verdict = %v, want Cloudflare (cf-mitigated header)", got)
	}
}

func TestDefaultDetector_DataDomeBlocked(t *testing.T) {
	// Real-shape DataDome block page: an iframe pointing at
	// captcha-delivery.com plus the `var dd=` global config object.
	body := []byte(`<!DOCTYPE html><html><body>` +
		`<script>var dd={'rt':'i','cid':'AHrl…','hsh':'…'};</script>` +
		`<iframe src="https://geo.captcha-delivery.com/captcha/?initialCid=…"></iframe>` +
		`</body></html>`)
	if got := DefaultDetector().Detect(body, nil); got != DataDome {
		t.Errorf("verdict = %v, want DataDome", got)
	}
}

// TestDataDome_LegitTagsJS_NotBlocked is THE load-bearing test for
// this package. Every legitimate MA / castorus / pappers page
// embeds the DataDome client SDK at //js.datadome.co/tags.js as a
// regular asset. The detector MUST NOT flag those pages — a regression
// here turns every successful enrich response into a 24h ErrAntiBot
// skip and silently kills the enricher.
func TestDataDome_LegitTagsJS_NotBlocked(t *testing.T) {
	body := []byte(`<!DOCTYPE html><html><head>` +
		`<script async src="//js.datadome.co/tags.js"></script>` +
		`</head><body>` +
		`<h1>Annonces immobilières</h1>` +
		`<div class="results">… 30 listings …</div>` +
		`</body></html>`)
	if got := DefaultDetector().Detect(body, nil); got != None {
		t.Fatalf("legit page carrying js.datadome.co/tags.js was flagged %v — false-positive regression", got)
	}
	// Belt-and-braces: the standalone DataDomeDetector must also pass.
	if got := (DataDomeDetector{}).Detect(body, nil); got != None {
		t.Fatalf("DataDomeDetector flagged tags.js asset — got %v want None", got)
	}
}

func TestDefaultDetector_Captcha(t *testing.T) {
	cases := map[string][]byte{
		"fr-not-robot":     []byte(`<html><body><p>Vous n'êtes pas un robot</p></body></html>`),
		"en-not-robot":     []byte(`<html><body>You are not a robot.</body></html>`),
		"verify-human":     []byte(`<html><body>Please verify you are human.</body></html>`),
		"please-enable-js": []byte(`<html><body><p>Please enable JS and disable any ad blocker</p></body></html>`),
	}
	d := DefaultDetector()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := d.Detect(body, nil); got != Captcha {
				t.Errorf("verdict = %v, want Captcha", got)
			}
		})
	}
}

func TestDefaultDetector_CleanPage(t *testing.T) {
	body := []byte(`<!DOCTYPE html><html><head><title>Annonces</title></head>` +
		`<body><h1>Bienvenue</h1><p>Trois biens à la une.</p></body></html>`)
	if got := DefaultDetector().Detect(body, nil); got != None {
		t.Errorf("verdict = %v, want None on clean page", got)
	}
}

func TestDefaultDetector_EmptyBody(t *testing.T) {
	d := DefaultDetector()
	if got := d.Detect(nil, nil); got != None {
		t.Errorf("nil body: verdict = %v, want None", got)
	}
	if got := d.Detect([]byte{}, nil); got != None {
		t.Errorf("empty body: verdict = %v, want None", got)
	}
	if got := d.Detect([]byte{}, http.Header{}); got != None {
		t.Errorf("empty body + empty headers: verdict = %v, want None", got)
	}
}

// TestVerdictPrecedence confirms that on a body carrying both a
// Cloudflare title and a DataDome block marker the composite returns
// Cloudflare (CF runs first). Documented order from the package doc.
func TestVerdictPrecedence(t *testing.T) {
	body := []byte(`<title>Just a moment...</title><script>var dd={};</script>`)
	if got := DefaultDetector().Detect(body, nil); got != Cloudflare {
		t.Errorf("composite precedence: got %v, want Cloudflare", got)
	}
	// Standalone detectors continue to flag their own regime.
	if got := (DataDomeDetector{}).Detect(body, nil); got != DataDome {
		t.Errorf("DataDomeDetector standalone: got %v, want DataDome", got)
	}
}

func TestVerdict_String(t *testing.T) {
	cases := map[Verdict]string{
		None:       "none",
		Cloudflare: "cloudflare",
		DataDome:   "datadome",
		Captcha:    "captcha",
	}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q want %q", v, got, want)
		}
	}
}

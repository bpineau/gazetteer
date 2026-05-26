// Package locservice is a gazetteer.Source that pulls the
// LocService Tensiomètre Locatif (rental-market tightness gauge) for a
// commune + optional logement type.
//
// # Strategy
//
// LocService exposes a server-rendered HTML page at
// `https://www.locservice.fr/tensiometre/`. The page form's inline
// jQuery handler builds a deterministic URL once the user picks a
// commune (via INSEE) and an optional logement type:
//
//	document.location.href = 'tensiometre-' + logement + $('#Insee').val() + '.html';
//
// where `logement` is empty for "all types", or one of {chambre,
// studio, T2, T3, T4, T5, F3, F4} (the form value with any trailing "+"
// stripped), suffixed with a dash.
//
// Concretely:
//
//	GET https://www.locservice.fr/tensiometre/tensiometre-75107.html        // all logement types
//	GET https://www.locservice.fr/tensiometre/tensiometre-T2-75107.html     // appartement T2
//	GET https://www.locservice.fr/tensiometre/tensiometre-chambre-75107.html
//
// The response is a static HTML page (ISO-8859-1 encoded). The
// tensiometer score is encoded as the filename of an "arrow" image
// pointing onto a static gauge image, e.g.
// `<img src="/images/tensiometre/fleche8.png" />` for "extremement
// tendu". Two arrows are emitted: the first for "Facilite a trouver
// une location" (= rental supply tightness, our "tension_score"), the
// second for "Budget des locataires" (= tenants' budget headroom, our
// "budget_score").
//
// The image set fleche0.png .. fleche8.png exists; fleche9.png returns
// 404. Score range is therefore 0..8 inclusive (9 positions).
//
// When the commune lacks enough activity, LocService renders a single
// sentence:
//
//	<p class="result0tensio">Le marche locatif n'est pas suffisamment actif a <ville> pour obtenir des donnees fiables</p>
//
// We surface this as confidence="low" + sample_size=0 (no arrow
// extracted).
//
// No private API endpoint exists — the rendered HTML *is* the data.
// Public endpoint, no auth, no quota documented.
//
// # Rhythm & rate-limit
//
// Standard rhythm. www.locservice.fr is served by a small
// infrastructure; transient timeouts in the 10-30 s range have been
// observed during peak hours. Per-host caps live in the CLI registry.
package locservice

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ParsedResult is what the parser extracts from one tensiometre HTML
// response. All fields are optional; the caller decides how to surface
// missing data. Internal — the Source's BuildResult projects it onto
// the public Result type (which uses snake_case JSON tags for
// persistence).
type ParsedResult struct {
	// HasData reports whether the page carried a usable measurement.
	// false when LocService returned its "marche pas suffisamment actif"
	// fallback.
	HasData bool

	// TensionScore is the raw 0..8 LocService arrow value for the
	// "Facilite a trouver une location" gauge (= rental supply
	// tightness; high means landlord-friendly).
	TensionScore int

	// BudgetScore is the raw 0..8 LocService arrow value for the
	// "Budget des locataires" gauge (= tenant solvency; high means many
	// candidates have the budget). Only the second arrow on the page.
	BudgetScore int

	// HasBudget reports whether BudgetScore was successfully extracted.
	HasBudget bool

	// Label is the tensiometer bucket derived from TensionScore.
	Label TensionLabel

	// Description is the first sentence of the rendered "analyseTensio"
	// paragraph, with HTML entities resolved. May be empty for the
	// no-data case.
	Description string

	// CityLabel is the commune name LocService used in the response
	// header, e.g. "Paris 07", "Riom". Useful for cross-checking the
	// INSEE we sent.
	CityLabel string

	// NoDataMessage carries the literal "marche pas suffisamment actif"
	// sentence when HasData is false. May be empty if the page
	// neither rendered a measurement nor the no-data sentence (treated
	// as a parse failure by the caller).
	NoDataMessage string
}

// ErrParse signals an unparseable response (neither a measurement nor
// the recognised no-data sentence). The Source wraps it as
// gazetteer.ErrUpstreamUnavailable.
var ErrParse = errors.New("locservice: cannot parse response")

var (
	// Two consecutive `<img src="..fleche<N>.png"...>` are emitted in
	// the rendered measurement table; we capture them in order.
	reArrow = regexp.MustCompile(`fleche(\d+)\.png`)

	// The first paragraph of "analyseTensio" carries the descriptive
	// sentence. We capture the inner content and clean entities.
	reAnalyseTensio = regexp.MustCompile(`(?s)class="analyseTensio"[^>]*><p[^>]*>(.*?)</p>`)

	// Strip any HTML tag.
	reTag = regexp.MustCompile(`<[^>]+>`)
)

// Parse extracts the tensiometer signal from one LocService HTML
// response body. The body is assumed to be ISO-8859-1 / Latin-1 (as
// served live by LocService); we decode bytes 1:1 to runes since all
// significant markup uses ASCII characters and HTML entities.
//
// Returns ErrParse when the response carries neither an arrow nor the
// recognised no-data sentence (i.e. structure has shifted unexpectedly).
func Parse(body []byte) (ParsedResult, error) {
	res := ParsedResult{}
	if len(body) == 0 {
		return res, ErrParse
	}
	s := decodeLatin1(body)

	res.CityLabel = extractCityLabel(s)

	// "no data" detection: a literal sentence rendered in the result0
	// div, e.g.
	//   Le marche locatif n'est pas suffisamment actif a <ville> pour
	//   obtenir des donnees fiables
	// We resolve HTML entities first so `pas suffisamment actif` is a
	// stable substring regardless of accent/apostrophe encoding.
	sDecoded := decodeEntities(s)
	const noDataMarker = "pas suffisamment actif"
	if idx := strings.Index(sDecoded, noDataMarker); idx >= 0 {
		// Capture the full sentence for traceability via a regex over
		// the decoded text. Decoded entities mean we can use plain
		// ASCII in the pattern.
		fullRe := regexp.MustCompile(`(?i)Le\s+march[eé][^<]{0,200}pour\s+obtenir\s+des\s+donn[eé]es\s+fiables`)
		if full := fullRe.FindString(sDecoded); full != "" {
			res.NoDataMessage = reWhitespace.ReplaceAllString(strings.TrimSpace(full), " ")
		} else {
			start := idx
			if start > 60 {
				start = idx - 60
			} else {
				start = 0
			}
			end := min(idx+len(noDataMarker)+60, len(sDecoded))
			res.NoDataMessage = strings.TrimSpace(sDecoded[start:end])
		}
		return res, nil
	}

	arrows := reArrow.FindAllStringSubmatch(s, -1)
	if len(arrows) == 0 {
		return res, ErrParse
	}
	first, err := strconv.Atoi(arrows[0][1])
	if err != nil || first < ScoreMin || first > ScoreMax {
		return res, ErrParse
	}
	res.HasData = true
	res.TensionScore = first
	res.Label = ScoreToLabel(first)
	if len(arrows) >= 2 {
		if second, err := strconv.Atoi(arrows[1][1]); err == nil && second >= ScoreMin && second <= ScoreMax {
			res.BudgetScore = second
			res.HasBudget = true
		}
	}

	if m := reAnalyseTensio.FindStringSubmatch(s); len(m) >= 2 {
		res.Description = firstSentence(decodeEntities(reTag.ReplaceAllString(m[1], " ")))
	}

	return res, nil
}

// ScoreToLabel maps the raw 0..8 arrow value to one of the 5 buckets
// described in the spec. The mapping is intentionally simple and
// roughly aligned with what the rendered French sentence says:
//
//	0..1 → "tres detendu"   (extremement favorable aux locataires)
//	2..3 → "detendu"        (plutot favorable aux locataires)
//	4    → "equilibre"      (equilibre / mid-range)
//	5..6 → "tendu"          (tendu pour les locataires)
//	7..8 → "tres tendu"     (extremement tendu pour les locataires)
//
// Out-of-range scores fall through to "equilibre" (the safe middle).
func ScoreToLabel(s int) TensionLabel {
	switch {
	case s <= 1:
		return LabelTresDetendu
	case s <= 3:
		return LabelDetendu
	case s == 4:
		return LabelEquilibre
	case s <= 6:
		return LabelTendu
	case s <= ScoreMax:
		return LabelTresTendu
	default:
		return LabelEquilibre
	}
}

// extractCityLabel returns the commune name LocService rendered in the
// "Analyse du marche locatif a <city>" header. Returns "" on miss.
func extractCityLabel(s string) string {
	re := regexp.MustCompile(`(?s)<h2[^>]*>Analyse du march[^<]*&agrave;\s*([^<]+)</h2>`)
	if m := re.FindStringSubmatch(s); len(m) >= 2 {
		return strings.TrimSpace(decodeEntities(m[1]))
	}
	return ""
}

// firstSentence returns the substring up to the first sentence break.
// Used to keep the embedded description short in the payload.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			// Include the punctuation.
			return strings.TrimSpace(s[:i+1])
		}
	}
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

// decodeLatin1 converts a byte slice assumed to be ISO-8859-1 to a
// Go string. Each byte maps to the corresponding rune.
func decodeLatin1(b []byte) string {
	out := make([]rune, len(b))
	for i, c := range b {
		out[i] = rune(c)
	}
	return string(out)
}

// decodeEntities resolves the handful of HTML entities LocService
// emits. We hand-roll the table to avoid an external dep — the page
// vocabulary is small and stable.
var entityReplacements = []string{
	"&eacute;", "é",
	"&egrave;", "è",
	"&ecirc;", "ê",
	"&agrave;", "à",
	"&acirc;", "â",
	"&ocirc;", "ô",
	"&ugrave;", "ù",
	"&ucirc;", "û",
	"&icirc;", "î",
	"&iuml;", "ï",
	"&ccedil;", "ç",
	"&laquo;", "«",
	"&raquo;", "»",
	"&euro;", "€",
	"&sup2;", "²",
	"&nbsp;", " ",
	"&rsquo;", "'",
	"&lsquo;", "'",
	"&#x27;", "'",
	"&#039;", "'",
	"&amp;", "&",
	"&quot;", "\"",
	"&lt;", "<",
	"&gt;", ">",
}

// reWhitespace collapses runs of whitespace to a single space.
var reWhitespace = regexp.MustCompile(`\s+`)

func decodeEntities(s string) string {
	r := strings.NewReplacer(entityReplacements...)
	out := r.Replace(s)
	return reWhitespace.ReplaceAllString(out, " ")
}

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
// The response is a server-rendered HTML page. Since the 2026-09
// redesign it is UTF-8 and each gauge is a CSS dial: the needle's
// position is a custom property on the needle element,
//
//	<span class="rental-tension-dial-needle" style="--rental-tension-angle: 113deg"></span>
//
// with nine positions 22.5 degrees apart, from 0deg to 180deg, and a
// verdict paragraph naming the zone the needle points at
// (`rental-tension-zone-very_tense` .. `rental-tension-zone-very_relaxed`).
// Two dials are emitted, in order: "Facilite a trouver une location"
// (= rental supply tightness, our "tension_score") and "Budget des
// locataires" (= tenants' budget headroom, our "budget_score"). The
// dials read in opposite directions: the first puts "tres difficile"
// (extremely tense) at 0deg, the second puts "tres tendu" at 0deg, so
// the tension score is 8 minus the needle position and the budget score
// is the position itself. Both scores keep the legacy 0..8 range and
// meaning: a high tension score is landlord-friendly, a high budget
// score means many candidates have the budget.
//
// Until 2026-09 the page was ISO-8859-1 and each score was the filename
// of an "arrow" image laid over a static gauge, e.g.
// `<img src="/images/tensiometre/fleche8.png" />` for "extremement
// tendu", fleche0.png .. fleche8.png (fleche9.png returned 404), the
// two arrows emitted in the same order as the dials today. The parser
// still reads that shape, so a rollback upstream would not break it.
//
// When the commune lacks enough activity, LocService renders a single
// sentence, today in a `rental-tension-empty-notice` paragraph, before
// in a `result0tensio` one:
//
//	Le marche locatif n'est pas suffisamment actif a <ville> pour obtenir des donnees fiables
//
// We surface this as confidence="low" + sample_size=0 (no score
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
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
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
	// Current shape: one needle per dial, its angle in a CSS custom
	// property. Captured in document order (tension first, budget
	// second).
	reNeedle = regexp.MustCompile(`rental-tension-dial-needle"[^>]*--rental-tension-angle:\s*([0-9.]+)deg`)

	// Legacy shape: two consecutive `<img src="..fleche<N>.png"...>` in
	// the rendered measurement table, captured in the same order.
	reArrow = regexp.MustCompile(`fleche(\d+)\.png`)

	// The first paragraph of the analysis carries the descriptive
	// sentence: `rental-tension-analysis` today, `analyseTensio` before.
	// We capture the inner content and clean entities.
	reAnalyseTensio = regexp.MustCompile(`(?s)class="(?:rental-tension-analysis|analyseTensio)"[^>]*>\s*<p[^>]*>(.*?)</p>`)

	// Strip any HTML tag.
	reTag = regexp.MustCompile(`<[^>]+>`)

	// Full sentence used by the "marche pas suffisamment actif"
	// fallback branch. Captures from "Le marché" through
	// "obtenir des données fiables" with a small slack window.
	reNoDataFull = regexp.MustCompile(`(?i)Le\s+march[eé][^<]{0,200}pour\s+obtenir\s+des\s+donn[eé]es\s+fiables`)

	// Header line LocService renders for the targeted commune, used
	// by extractCityLabel. The heading may open with an icon element
	// and, for a logement-specific page, end with "pour les <type>".
	reCityLabel = regexp.MustCompile(`(?s)<h2[^>]*>(?:\s*<[^>]+>)*\s*Analyse du march[^<]*?(?:&agrave;|à)\s*([^<]+)</h2>`)
)

// Parse extracts the tensiometer signal from one LocService HTML
// response body. The body is UTF-8 since the 2026-09 redesign and was
// ISO-8859-1 / Latin-1 before; a body that is not valid UTF-8 is decoded
// byte for byte, which is exact for Latin-1 and harmless for the
// markup, all ASCII.
//
// Returns ErrParse when the response carries neither a dial, nor an
// arrow, nor the recognised no-data sentence (i.e. the structure has
// shifted unexpectedly).
func Parse(body []byte) (ParsedResult, error) {
	res := ParsedResult{}
	if len(body) == 0 {
		return res, ErrParse
	}
	s := decodeBody(body)

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
		// Capture the full sentence for traceability via the
		// package-level reNoDataFull regex over the decoded text.
		if full := reNoDataFull.FindString(sDecoded); full != "" {
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

	scores, ok := dialScores(s)
	if !ok {
		scores, ok = arrowScores(s)
	}
	if !ok {
		return res, ErrParse
	}
	res.HasData = true
	res.TensionScore = scores[0]
	res.Label = ScoreToLabel(scores[0])
	if len(scores) >= 2 {
		res.BudgetScore = scores[1]
		res.HasBudget = true
	}

	if m := reAnalyseTensio.FindStringSubmatch(s); len(m) >= 2 {
		res.Description = firstSentence(decodeEntities(reTag.ReplaceAllString(m[1], " ")))
	}

	return res, nil
}

// dialStepDegrees is the angle between two adjacent needle positions on
// the current dials: nine positions over a half turn.
const dialStepDegrees = 180.0 / float64(ScoreMax-ScoreMin)

// dialScores reads the current markup: the needle angles in document
// order, converted to the legacy 0..8 scale. The first dial (tension)
// puts its tense end at 0deg, so its score is reversed; the second
// (budget) puts its relaxed end at 180deg, so its score is the position
// itself. Reports false when no needle is rendered or an angle is out of
// range.
func dialScores(s string) ([]int, bool) {
	m := reNeedle.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil, false
	}
	scores := make([]int, 0, len(m))
	for i, sub := range m {
		deg, err := strconv.ParseFloat(sub[1], 64)
		if err != nil || deg < 0 || deg > 180 {
			return nil, false
		}
		pos := int(math.Round(deg / dialStepDegrees))
		if i == 0 {
			pos = ScoreMax - pos
		}
		scores = append(scores, pos)
	}
	return scores, true
}

// arrowScores reads the legacy markup: the arrow image indices in
// document order, already on the 0..8 scale. Reports false when no
// arrow is rendered or the first index is out of range; a bad second
// index only drops the budget score, as the legacy parser did.
func arrowScores(s string) ([]int, bool) {
	arrows := reArrow.FindAllStringSubmatch(s, -1)
	if len(arrows) == 0 {
		return nil, false
	}
	first, err := strconv.Atoi(arrows[0][1])
	if err != nil || first < ScoreMin || first > ScoreMax {
		return nil, false
	}
	scores := []int{first}
	if len(arrows) >= 2 {
		if second, err := strconv.Atoi(arrows[1][1]); err == nil && second >= ScoreMin && second <= ScoreMax {
			scores = append(scores, second)
		}
	}
	return scores, true
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
	if m := reCityLabel.FindStringSubmatch(s); len(m) >= 2 {
		label := decodeEntities(m[1])
		// A logement-specific page appends the type: "à Limoges pour
		// les chambres". Only the commune is the label.
		if i := strings.Index(label, " pour "); i >= 0 {
			label = label[:i]
		}
		return strings.TrimSpace(label)
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

// decodeBody turns a response body into a string: as is when it is
// valid UTF-8 (the page since 2026-09), byte for byte otherwise (the
// ISO-8859-1 page before, where each byte is the rune of the same
// value).
func decodeBody(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return decodeLatin1(b)
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

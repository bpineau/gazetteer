package encadrement

import (
	"strconv"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/helpers/frnorm"
)

// The published grilles bucket dwellings by construction period ("époque de
// construction"), and the cap genuinely differs from one bucket to the next:
// in Paris 1er, a 3-room flat is capped at 28.1 EUR/m2/month when the building
// went up in 1946-1970 and at 35.0 when it predates 1946, a 25 % spread on the
// same cell. Matching the listing's BuildYear to its bucket is therefore part
// of reading the right cap, not a refinement.
//
// Three vocabularies ship in the embedded artifacts and none of them agrees
// with the others on case or accents:
//
//	Paris            "Avant 1946" "1946-1970" "1971-1990" "Apres 1990"
//	Plaine Commune / "avant 1946" "1946-1970" "1971-1990" "apres 1990"
//	Est Ensemble
//	Lyon             "avant 1946" "1946-1970" "1971-1990" "1991-2005" "après 2005"
//
// epoqueRange parses all three into an inclusive [lo, hi] year range, so the
// matcher works off the label's own arithmetic rather than a hand-maintained
// table that would silently rot the next time a territory joins with a fourth
// spelling.

// minEpoqueYear / maxEpoqueYear are the open ends of an unbounded bucket
// ("avant 1946" has no lower bound, "après 2005" no upper one). They are plain
// years rather than math.MinInt/MaxInt so a range is always printable.
const (
	minEpoqueYear = -9999
	maxEpoqueYear = 9999
)

// plausibleBuildYear bounds what counts as a usable Listing.BuildYear. Below
// the floor the value is a typo or a sentinel (0 for "unknown"); above the
// ceiling it is a data-entry slip. A dwelling sold off-plan (VEFA) legitimately
// carries a completion year a few years out, hence the forward allowance.
const (
	minPlausibleBuildYear = 1000
	buildYearLookahead    = 5
)

// epoqueRange parses a published époque label into the inclusive [lo, hi]
// range of construction years it covers. ok is false for a label whose shape
// it does not recognise, in which case the caller must not filter on it.
//
// Recognised shapes, accent- and case-insensitive:
//
//	"avant 1946"  -> [minEpoqueYear, 1945]   (strictly before)
//	"apres 1990"  -> [1991, maxEpoqueYear]   (strictly after)
//	"1946-1970"   -> [1946, 1970]            (inclusive on both ends)
//
// The strict reading of "avant"/"après" is what makes the published buckets
// tile the timeline without a gap or an overlap: 1945 belongs to "avant 1946",
// 1946 to "1946-1970", 1991 to "après 1990".
func epoqueRange(label string) (lo, hi int, ok bool) {
	s := strings.ToLower(strings.TrimSpace(frnorm.StripAccents(label)))
	switch {
	case strings.HasPrefix(s, "avant"):
		y, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "avant")))
		if err != nil {
			return 0, 0, false
		}
		return minEpoqueYear, y - 1, true
	case strings.HasPrefix(s, "apres"):
		y, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "apres")))
		if err != nil {
			return 0, 0, false
		}
		return y + 1, maxEpoqueYear, true
	}
	from, to, found := strings.Cut(s, "-")
	if !found {
		return 0, 0, false
	}
	a, err := strconv.Atoi(strings.TrimSpace(from))
	if err != nil {
		return 0, 0, false
	}
	b, err := strconv.Atoi(strings.TrimSpace(to))
	if err != nil || b < a {
		return 0, 0, false
	}
	return a, b, true
}

// epoqueCovers reports whether the bucket named by label contains year.
// A label epoqueRange cannot parse covers nothing, so an unrecognised
// vocabulary degrades to "no cell matched" (and the caller falls back to the
// all-époques collapse) rather than to a silently wrong cell.
func epoqueCovers(label string, year int) bool {
	lo, hi, ok := epoqueRange(label)
	return ok && year >= lo && year <= hi
}

// usableBuildYear normalises a Listing.BuildYear into the year to filter on.
// It returns 0 — "époque unknown, span every bucket" — for a nil pointer and
// for any value outside [minPlausibleBuildYear, this year + buildYearLookahead].
func usableBuildYear(p *int, now time.Time) int {
	if p == nil {
		return 0
	}
	y := *p
	if y < minPlausibleBuildYear || y > now.Year()+buildYearLookahead {
		return 0
	}
	return y
}

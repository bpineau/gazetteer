// Package communes exposes a lookup table over the ~35 000 French
// communes (including the Paris/Lyon/Marseille arrondissements).
//
// Source: https://geo.api.gouv.fr/communes (snapshot embedded as
// data/france.csv at build time). Format: insee,dept,lon,lat,name.
//
// The table supports INSEE -> Commune lookup, neighbor search by
// haversine radius, same-department enumeration and reverse city-name
// -> department lookup.
package communes

import (
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/bpineau/gazetteer/helpers/geodist"
)

// Commune is the in-memory record for one row of france.csv.
type Commune struct {
	INSEE string
	Dept  string
	Lon   float64
	Lat   float64
	Name  string
}

// Communes is the contract every consumer talks to. Concrete impl:
// the file-backed Table built by NewTable / NewEmbedded.
type Communes interface {
	// Lookup returns the Commune for an INSEE code, or (zero, false).
	Lookup(insee string) (Commune, bool)

	// Neighbors returns the INSEE codes of communes whose centroid is
	// within radiusKm of the centroid of `insee`. Always includes
	// `insee` itself when it is known.
	Neighbors(insee string, radiusKm float64) []string

	// SameDepartment returns the INSEE codes of every commune sharing
	// the same department code as `insee`.
	SameDepartment(insee string) []string
}

// Table is a slice-backed Communes implementation. Cheap to construct
// (~35 000 rows in ~1.5 MB), cheap to query (linear scans for
// Neighbors/SameDepartment, indexed for Lookup).
type Table struct {
	rows  []Commune
	byID  map[string]int      // INSEE -> rows index
	byDpt map[string][]string // dept code -> sorted list of INSEE codes

	// byNameNormOnce guards lazy construction of byNameNorm. The
	// city-name index is only needed by callers that do reverse
	// city → dept lookups; keeping it lazy avoids a ~35 000-entry map
	// for the 99 % of users who never reach for it.
	byNameNormOnce sync.Once
	byNameNorm     map[string][]string // normalized name -> sorted unique dept codes

	// byNameDeptOnce guards lazy construction of byNameDept. The
	// (name, dept) index is only needed by callers that do reverse
	// city + dept → INSEE lookups (used by CityZip for the licitor
	// city→zip fallback). Built on first use of CityZip.
	byNameDeptOnce sync.Once
	byNameDept     map[string][]string // (normName + "\x00" + dept) -> sorted INSEE list
}

// NewTable builds a Table from a slice. The slice is copied; callers may
// mutate their original after the call. Departments are pre-indexed.
func NewTable(rows []Commune) *Table {
	t := &Table{
		rows:  make([]Commune, len(rows)),
		byID:  make(map[string]int, len(rows)),
		byDpt: make(map[string][]string),
	}
	copy(t.rows, rows)
	for i, c := range t.rows {
		t.byID[c.INSEE] = i
		t.byDpt[c.Dept] = append(t.byDpt[c.Dept], c.INSEE)
	}
	for d := range t.byDpt {
		sort.Strings(t.byDpt[d])
	}
	return t
}

// Lookup implements Communes.Lookup.
func (t *Table) Lookup(insee string) (Commune, bool) {
	if t == nil {
		return Commune{}, false
	}
	i, ok := t.byID[insee]
	if !ok {
		return Commune{}, false
	}
	return t.rows[i], true
}

// Neighbors implements Communes.Neighbors. Linear scan over the rows of
// the same department first (cheap O(K) where K = ~600 for a typical
// department). For very large radii this fans out across departments.
func (t *Table) Neighbors(insee string, radiusKm float64) []string {
	if t == nil {
		return nil
	}
	c, ok := t.Lookup(insee)
	if !ok {
		return nil
	}
	if radiusKm <= 0 {
		return []string{insee}
	}
	out := []string{insee}
	seen := map[string]struct{}{insee: {}}

	// Most callers stay within the same department; check it first.
	for _, otherINSEE := range t.byDpt[c.Dept] {
		if otherINSEE == insee {
			continue
		}
		other := t.rows[t.byID[otherINSEE]]
		if HaversineKm(c.Lat, c.Lon, other.Lat, other.Lon) <= radiusKm {
			if _, dup := seen[otherINSEE]; !dup {
				out = append(out, otherINSEE)
				seen[otherINSEE] = struct{}{}
			}
		}
	}

	// For radii > ~10 km, also widen across departments (e.g. for IDF
	// arrondissements near a department border). Bounded full scan.
	if radiusKm > 10 {
		for i := range t.rows {
			other := t.rows[i]
			if other.Dept == c.Dept {
				continue
			}
			if HaversineKm(c.Lat, c.Lon, other.Lat, other.Lon) <= radiusKm {
				if _, dup := seen[other.INSEE]; !dup {
					out = append(out, other.INSEE)
					seen[other.INSEE] = struct{}{}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// CityDepts returns the sorted, deduplicated list of department codes
// in which a commune with the given (free-form) name exists. Matching
// is on a normalized form (accent-folded, lowercased, alphanumerics
// only) so callers can pass user input directly. Returns nil for an
// unknown city or a nil receiver.
//
// Useful for guards that need to verify a city-name + postcode pair
// (e.g. reject a city upgraded to "75016" when the city only exists in
// dept 91).
//
// The arrondissement entries (Paris 1er .. 20e, Lyon 1er .. 9e,
// Marseille 1er .. 16e) appear under their full name "paris1earrondissement"
// etc., not under the bare "paris" — callers that pass the bare
// arrondissement-city should expect a miss and skip the guard rather
// than reject.
func (t *Table) CityDepts(name string) []string {
	if t == nil {
		return nil
	}
	key := normalizeCityName(name)
	if key == "" {
		return nil
	}
	t.byNameNormOnce.Do(func() {
		// Build name -> set(dept) on first use.
		idx := make(map[string]map[string]struct{}, len(t.rows))
		for i := range t.rows {
			c := &t.rows[i]
			k := normalizeCityName(c.Name)
			if k == "" {
				continue
			}
			set, ok := idx[k]
			if !ok {
				set = make(map[string]struct{}, 1)
				idx[k] = set
			}
			set[c.Dept] = struct{}{}
		}
		out := make(map[string][]string, len(idx))
		for k, set := range idx {
			lst := make([]string, 0, len(set))
			for d := range set {
				lst = append(lst, d)
			}
			sort.Strings(lst)
			out[k] = lst
		}
		t.byNameNorm = out
	})
	depts, ok := t.byNameNorm[key]
	if !ok {
		return nil
	}
	// Defensive copy to keep the cached slice immutable.
	dup := make([]string, len(depts))
	copy(dup, depts)
	return dup
}

// normalizeCityName produces a stable lookup key for CityDepts. Folds
// accents to ASCII (best-effort: NFKD-light over the Latin-1 range),
// lowercases, and strips every non-alphanumeric rune. Mirrors the
// lighter-weight side of matching.CityNormalized (which lives in a
// downstream package and can't be imported from here without a cycle).
func normalizeCityName(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'à', r == 'â', r == 'ä':
			b.WriteRune('a')
		case r == 'é', r == 'è', r == 'ê', r == 'ë':
			b.WriteRune('e')
		case r == 'î', r == 'ï':
			b.WriteRune('i')
		case r == 'ô', r == 'ö':
			b.WriteRune('o')
		case r == 'ù', r == 'û', r == 'ü':
			b.WriteRune('u')
		case r == 'ÿ':
			b.WriteRune('y')
		case r == 'ç':
			b.WriteRune('c')
		case unicode.IsLetter(r):
			// Any other letter we can't fold cheaply: drop it. Cheap
			// approximation — keeps the key shape for ASCII-heavy
			// inputs (which dominates a French commune corpus).
		}
	}
	return b.String()
}

// SameDepartment implements Communes.SameDepartment.
func (t *Table) SameDepartment(insee string) []string {
	if t == nil {
		return nil
	}
	c, ok := t.Lookup(insee)
	if !ok {
		return nil
	}
	out := make([]string, len(t.byDpt[c.Dept]))
	copy(out, t.byDpt[c.Dept])
	return out
}

// NearestDept returns the département code of the commune whose centroid
// is closest to `(lat, lon)`. Returns "" when the table is empty or the
// inputs are unset (lat == 0 && lon == 0 — the geocoder sentinel handled
// elsewhere). The scan is O(N) over ~35 000 rows (~5 ms on commodity
// hardware) — fine for the per-listing scrape hot path.
//
// Used by the source mappers (vench, licitor) to reject lat/lon emitted
// for a property when the point falls in a different département than
// the property's zip — a class of bug observed when source HTML
// embeds the wrong cadastre iframe / gmap pin.
func (t *Table) NearestDept(lat, lon float64) string {
	if t == nil || len(t.rows) == 0 {
		return ""
	}
	if lat == 0 && lon == 0 {
		return ""
	}
	var bestDept string
	bestDist := math.MaxFloat64
	for i := range t.rows {
		d := HaversineKm(lat, lon, t.rows[i].Lat, t.rows[i].Lon)
		if d < bestDist {
			bestDist = d
			bestDept = t.rows[i].Dept
		}
	}
	return bestDept
}

// HaversineKm returns the great-circle distance between two (lat, lon)
// points in kilometers. Historical re-export of [geodist.KmBetween]:
// `helpers/communes` was the only public package exposing this utility,
// so existing callers depend on it. New callers should import
// `helpers/geodist` directly to avoid pulling the embedded INSEE CSV
// (~100 KB) along with them.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	return geodist.KmBetween(lat1, lon1, lat2, lon2)
}

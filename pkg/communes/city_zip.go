package communes

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

//go:embed data/insee_cp.csv
var inseeCPCSV []byte

// InseeCPCSVBytes returns the raw embedded INSEE -> CP CSV. Useful for
// tools that want to inspect the source.
func InseeCPCSVBytes() []byte { return inseeCPCSV }

// cpStore is a lazily-built map from INSEE -> primary postal code, with
// an optional list of alternate codes. Populated from the embedded
// `data/insee_cp.csv` on first call to any CP lookup. Decoupled from the
// main Table loader because most consumers of `communes` never reach for
// postal codes.
type cpStore struct {
	primary map[string]string   // insee -> single primary CP
	alts    map[string][]string // insee -> sorted alternate CPs (excl. primary)
}

var (
	cpOnce  sync.Once
	cpData  *cpStore
	cpError error
)

func loadCP() (*cpStore, error) {
	cpOnce.Do(func() {
		cpData, cpError = parseInseeCPCSV(bytes.NewReader(inseeCPCSV))
	})
	return cpData, cpError
}

// parseInseeCPCSV reads the `insee,cp,cps_alt` CSV embedded under
// data/insee_cp.csv and returns the in-memory store. The first record
// must be the header.
func parseInseeCPCSV(r io.Reader) (*cpStore, error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	first, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("communes: read insee_cp header: %w", err)
	}
	if len(first) < 2 || first[0] != "insee" || first[1] != "cp" {
		return nil, fmt.Errorf("communes: unexpected insee_cp header %v", first)
	}
	out := &cpStore{
		primary: make(map[string]string, 35000),
		alts:    make(map[string][]string),
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("communes: parse insee_cp row: %w", err)
		}
		if len(rec) < 2 {
			continue
		}
		insee := rec[0]
		cp := rec[1]
		if insee == "" || cp == "" {
			continue
		}
		out.primary[insee] = cp
		if len(rec) >= 3 && rec[2] != "" {
			alts := strings.Split(rec[2], "|")
			out.alts[insee] = alts
		}
	}
	if len(out.primary) == 0 {
		return nil, fmt.Errorf("communes: empty insee_cp CSV")
	}
	return out, nil
}

// ZipForINSEE returns the primary 5-digit French postal code for an
// INSEE code, along with a boolean indicating whether there are
// additional ("alternate") postal codes associated with the same INSEE.
// Returns ("", false, false) for unknown INSEE codes, nil receivers, or
// when the embedded CP dataset fails to load.
//
// "Ambiguous" callers can prefer ZipForINSEE-then-decide-not-to-use
// over a naive single-zip API, since some INSEE codes (e.g. 75056 for
// the consolidated city of Paris) carry one CP per arrondissement.
func (t *Table) ZipForINSEE(insee string) (cp string, ambiguous bool, ok bool) {
	if t == nil || insee == "" {
		return "", false, false
	}
	store, err := loadCP()
	if err != nil || store == nil {
		return "", false, false
	}
	cp, ok = store.primary[insee]
	if !ok {
		return "", false, false
	}
	_, ambiguous = store.alts[insee]
	return cp, ambiguous, true
}

// AltZipsForINSEE returns the sorted list of alternate postal codes
// (excluding the primary) for the given INSEE, or nil. Mostly useful
// for callers that need to enumerate all CPs a commune is split across
// (e.g. Paris 75056 -> 75001..75020 + 75116).
func (t *Table) AltZipsForINSEE(insee string) []string {
	if t == nil || insee == "" {
		return nil
	}
	store, err := loadCP()
	if err != nil || store == nil {
		return nil
	}
	alts, ok := store.alts[insee]
	if !ok {
		return nil
	}
	dup := make([]string, len(alts))
	copy(dup, alts)
	return dup
}

// CityZip returns the unambiguous 5-digit postal code for a (city name,
// department) pair, or "" when:
//   - the name doesn't match any commune in `dept`
//   - more than one commune in `dept` shares the normalized name
//   - the resolved commune has multiple postal codes (ambiguous CP)
//   - the embedded CP dataset doesn't carry a CP for that INSEE
//   - the receiver is nil
//
// Inputs are matched against the same accent-folded normalization as
// CityDepts: "Vierzon" / "VIERZON" / "vierzon" all hit the same row.
// `dept` is the 2-digit (or 3-digit DOM-TOM) department prefix as used
// throughout the codebase (Lot.ZipArea in the licitor mapper).
//
// Concrete examples:
//
//	CityZip("Vierzon", "18")             = "18100", true
//	CityZip("Saint-Amand-Montrond", "18") = "18200", true
//	CityZip("Perceneige", "89")          = "89260", true
//	CityZip("Paris", "75")               = "", false    (multi-CP)
//	CityZip("Vincennes", "94")           = "94300", true
//
// The function is read-only and safe for concurrent use; the underlying
// name-dept index is built lazily on first call.
func (t *Table) CityZip(name, dept string) (string, bool) {
	if t == nil {
		return "", false
	}
	key := normalizeCityName(name)
	if key == "" || dept == "" {
		return "", false
	}
	t.byNameDeptOnce.Do(t.buildByNameDept)
	dKey := nameDeptKey(key, dept)
	insees, ok := t.byNameDept[dKey]
	if !ok || len(insees) == 0 {
		return "", false
	}
	if len(insees) > 1 {
		// Multiple communes in the same dept share the normalized name.
		// Rare (none observed in metropolitan France for the audit
		// cohort) but the conservative posture is to refuse rather
		// than guess.
		return "", false
	}
	cp, ambiguous, found := t.ZipForINSEE(insees[0])
	if !found || ambiguous || cp == "" {
		return "", false
	}
	return cp, true
}

// INSEEByCityDept returns the unambiguous 5-digit INSEE for a (city
// name, department) pair, or "" when : the name doesn't match any
// commune in `dept`, multiple communes in `dept` share the normalized
// name (ambiguous), or the receiver is nil. Mirrors CityZip's
// disambiguation rules — the conservative posture is to refuse rather
// than guess when several INSEEs collide.
//
// Used by callers that need the canonical commune code (e.g. for an
// embedded dataset indexed by INSEE) without going through the postal
// code detour.
func (t *Table) INSEEByCityDept(name, dept string) (string, bool) {
	if t == nil {
		return "", false
	}
	key := normalizeCityName(name)
	if key == "" || dept == "" {
		return "", false
	}
	t.byNameDeptOnce.Do(t.buildByNameDept)
	insees, ok := t.byNameDept[nameDeptKey(key, dept)]
	if !ok || len(insees) == 0 {
		return "", false
	}
	if len(insees) > 1 {
		return "", false
	}
	return insees[0], true
}

// nameDeptKey is the composite key used by the byNameDept index.
// Separator is "\x00" to guarantee no collision with normalized names
// (which only carry alphanumerics).
func nameDeptKey(normName, dept string) string {
	return normName + "\x00" + dept
}

// buildByNameDept populates t.byNameDept under the protection of
// byNameDeptOnce. Each key is (normalized name + dept) and the value is
// the sorted list of INSEE codes that share that pair. Most pairs map
// to a single INSEE; cases with >1 are surfaced via the len()>1 guard
// in CityZip.
func (t *Table) buildByNameDept() {
	idx := make(map[string]map[string]struct{}, len(t.rows))
	for i := range t.rows {
		c := &t.rows[i]
		k := normalizeCityName(c.Name)
		if k == "" || c.Dept == "" {
			continue
		}
		dk := nameDeptKey(k, c.Dept)
		set, ok := idx[dk]
		if !ok {
			set = make(map[string]struct{}, 1)
			idx[dk] = set
		}
		set[c.INSEE] = struct{}{}
	}
	out := make(map[string][]string, len(idx))
	for k, set := range idx {
		lst := make([]string, 0, len(set))
		for ins := range set {
			lst = append(lst, ins)
		}
		sort.Strings(lst)
		out[k] = lst
	}
	t.byNameDept = out
}

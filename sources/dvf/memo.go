package dvf

import "sync"

// queryMemo is the per-Query fetch memoisation shared by every rung of
// one ladder walk. The four DVF tiers are geographic supersets of each
// other (communes.Neighbors INCLUDES the primary commune;
// SameDepartment includes the primary and most neighbours), so before
// this memo a fall-through re-fetched the primary commune's sections up
// to 4× and every neighbour 2× — dozens-to-hundreds of redundant HTTP
// GETs on exactly the low-data addresses that reach the deep tiers.
// Memoising the post-parse payloads caps each (insee, section) mutation
// fetch, per-insee section list and per-insee geometry download at once
// per Query.
//
// Scope is one Query call: cross-Query persistence stays in the kvcache
// (SectionDiscoverer). Mutation lists are deliberately NOT persisted
// across Queries — they are the freshest, heaviest payloads.
//
// Only definitive outcomes are stored: a successful fetch, or a 404
// (ErrSectionNotFound ⇒ the section has no DVF data, stored as nil).
// Transient per-section failures are not memoised, so a later tier
// retries them exactly as before.
// They are recorded instead (recordFailure), so the walk can tell an
// empty cohort from an unavailable one.
//
// The stored slices are shared across tiers and must be treated as
// read-only — every consumer copies the elements (append into a fresh
// pool) before filtering, and nothing in the package writes through a
// Mutation's pointer fields.
//
// Concurrency: fetchSections fans goroutines out within one tier, so
// the maps are mutex-guarded. Tiers themselves run sequentially
// (fallback.Walk). All methods are nil-receiver-safe — a nil memo
// disables memoisation, which keeps the fetch paths drivable without
// one (tests, future callers).
type queryMemo struct {
	mu        sync.Mutex
	mutations map[string][]Mutation   // insee+"/"+section → post-parse mutations
	sections  map[string][]string     // insee → DVF section codes
	geos      map[string][]SectionGeo // insee → reduced section geometries

	// failed holds the fetches that failed non-definitively and were not
	// retried successfully since (key → last error). See recordFailure.
	failed map[string]error
}

func newQueryMemo() *queryMemo {
	return &queryMemo{
		mutations: make(map[string][]Mutation),
		sections:  make(map[string][]string),
		geos:      make(map[string][]SectionGeo),
	}
}

func (m *queryMemo) mutationsFor(insee, section string) ([]Mutation, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	muts, ok := m.mutations[insee+"/"+section]
	return muts, ok
}

func (m *queryMemo) storeMutations(insee, section string, muts []Mutation) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mutations[insee+"/"+section] = muts
	m.clearFailure(insee + "/" + section)
}

func (m *queryMemo) sectionsFor(insee string) ([]string, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	secs, ok := m.sections[insee]
	return secs, ok
}

func (m *queryMemo) storeSections(insee string, secs []string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sections[insee] = secs
	m.clearFailure(insee + "/*")
}

func (m *queryMemo) geosFor(insee string) ([]SectionGeo, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	geos, ok := m.geos[insee]
	return geos, ok
}

func (m *queryMemo) storeGeos(insee string, geos []SectionGeo) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.geos[insee] = geos
}

// recordFailure notes a non-definitive fetch failure (transport error,
// 5xx, timeout) for key, an "insee/section" pair or an "insee/*" section
// listing. The failure stands until a later tier fetches the same key
// successfully (storeMutations / storeSections clear it), so at the end of
// the walk the record holds exactly the data the cohort is missing.
func (m *queryMemo) recordFailure(key string, err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed == nil {
		m.failed = make(map[string]error)
	}
	m.failed[key] = err
}

// clearFailure drops a failure record once its key was fetched for good.
// The caller holds m.mu.
func (m *queryMemo) clearFailure(key string) {
	delete(m.failed, key)
}

// failures returns how many fetches are still unresolved and one of their
// errors (the lexically first key's, so the choice is deterministic).
func (m *queryMemo) failures() (int, error) {
	if m == nil {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstKey string
	for k := range m.failed {
		if firstKey == "" || k < firstKey {
			firstKey = k
		}
	}
	if firstKey == "" {
		return 0, nil
	}
	return len(m.failed), m.failed[firstKey]
}

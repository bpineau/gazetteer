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

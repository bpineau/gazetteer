package gazetteer

import (
	"fmt"
	"sort"
	"sync"
)

var (
	regMu    sync.RWMutex
	registry = map[string]func() any{}
)

// Register associates a Source name with a factory that returns a fresh,
// zero-valued typed Data value (e.g. &dvf.Result{}). Sources call this
// in their init() so that Dossier JSON roundtrip can reconstitute
// concrete types from a wire payload.
//
// Duplicate registration panics — this is a programming error caught at
// startup, not a runtime condition. (Mirrors database/sql's Register.)
func Register(name string, factory func() any) {
	if name == "" {
		panic("gazetteer.Register: empty name")
	}
	if factory == nil {
		panic("gazetteer.Register: nil factory")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("gazetteer.Register: duplicate registration for %q", name))
	}
	registry[name] = factory
}

// Lookup returns the registered factory for name, or nil if unknown.
func Lookup(name string) func() any {
	regMu.RLock()
	defer regMu.RUnlock()
	return registry[name]
}

// RegisteredNames returns the sorted list of every name that has been
// passed to Register. Used by the CLI's `sources list` sub-command and
// any caller that needs to walk the catalogue without hard-coding it.
// The returned slice is a freshly allocated copy — callers can mutate
// it without affecting the registry.
func RegisteredNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

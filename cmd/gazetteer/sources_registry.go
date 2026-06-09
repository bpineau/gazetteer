package main

import (
	"fmt"
	"sort"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/internal/roster"
)

// sourceFactory is the CLI-side view of one roster entry: the shared
// constructor plus the CLI's default-selection policy. The enumeration
// itself lives in internal/roster — the single list the library factory
// and this CLI both consume — so the CLI can never know a different set
// of sources than factory.NewDefault wires.
type sourceFactory struct {
	Name    string
	Build   func(deps *runtimeDeps) (gazetteer.Source, error)
	Default bool // included when --source is unset
}

// sourceCatalog returns the registry of source factories the CLI
// exposes, in the roster's curated thematic order. The function returns
// a fresh slice on each call so callers can mutate / filter it without
// affecting peers.
//
// Defaults: every source except the roster's CLIOptIn entries (today
// bdnb — its public endpoint throttles anonymous traffic; use
// `--source bdnb` when the building-attributes signal matters).
func sourceCatalog() []sourceFactory {
	entries := roster.Entries()
	out := make([]sourceFactory, 0, len(entries))
	for _, e := range entries {
		out = append(out, sourceFactory{
			Name:    e.Name,
			Default: !e.CLIOptIn,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return e.Build(d.rosterDeps())
			},
		})
	}
	return out
}

// allSourceNames returns the catalog's source names in registration
// order. Used by `sources list` and the `--source` flag's help text.
func allSourceNames() []string {
	cat := sourceCatalog()
	out := make([]string, len(cat))
	for i, f := range cat {
		out[i] = f.Name
	}
	return out
}

// resolveSources filters the catalog by the comma-separated names in
// `selected` (empty = use Default-tagged entries). Returns the
// instantiated Source slice ready to feed into a gazetteer.Builder.
// Unknown names yield an error listing what's available.
func resolveSources(deps *runtimeDeps, selected []string) ([]gazetteer.Source, error) {
	cat := sourceCatalog()
	byName := make(map[string]sourceFactory, len(cat))
	for _, f := range cat {
		byName[f.Name] = f
	}

	var picks []sourceFactory
	if len(selected) == 0 {
		for _, f := range cat {
			if f.Default {
				picks = append(picks, f)
			}
		}
	} else {
		for _, name := range selected {
			f, ok := byName[name]
			if !ok {
				avail := allSourceNames()
				sort.Strings(avail)
				return nil, fmt.Errorf("unknown source %q (available: %v)", name, avail)
			}
			picks = append(picks, f)
		}
	}

	out := make([]gazetteer.Source, 0, len(picks))
	for _, f := range picks {
		s, err := f.Build(deps)
		if err != nil {
			return nil, fmt.Errorf("build source %q: %w", f.Name, err)
		}
		out = append(out, s)
	}
	return out, nil
}

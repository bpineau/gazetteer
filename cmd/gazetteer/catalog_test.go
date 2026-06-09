package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestCatalogCompleteness is the anti-drift guard: the curated descriptor set
// must exactly match the set of registered sources. A new source with no
// descriptor (or a descriptor for a removed source) fails the build — so the
// machine-readable catalog an AI agent ingests can never silently lie.
func TestCatalogCompleteness(t *testing.T) {
	registered := make(map[string]bool)
	for _, name := range gazetteer.RegisteredNames() {
		registered[name] = true
		if _, ok := sourceDescriptors[name]; !ok {
			t.Errorf("registered source %q has no catalog descriptor — add one in catalog.go", name)
		}
	}
	for name := range sourceDescriptors {
		if !registered[name] {
			t.Errorf("catalog descriptor %q is not a registered source — stale entry in catalog.go", name)
		}
	}
}

// TestCatalogWellFormed checks each descriptor carries the minimum an agent
// needs: a summary, at least one input, and a coverage statement.
func TestCatalogWellFormed(t *testing.T) {
	for name, d := range sourceDescriptors {
		if d.Summary == "" {
			t.Errorf("%s: empty summary", name)
		}
		if len(d.Inputs) == 0 {
			t.Errorf("%s: no inputs listed", name)
		}
		if d.Coverage == "" {
			t.Errorf("%s: no coverage stated", name)
		}
	}
}

// TestSourcesJSONCurrent is the strongest anti-drift guard: the committed
// docs/sources.json (which agents read straight from the repo) must equal the
// live catalog byte-for-byte. If this fails, regenerate it:
//
//	go run ./cmd/gazetteer sources catalog --json > docs/sources.json
func TestSourcesJSONCurrent(t *testing.T) {
	const path = "../../docs/sources.json"
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildCatalog()); err != nil {
		t.Fatalf("encode catalog: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(buf.Bytes())) {
		t.Errorf("%s is stale — regenerate:\n\tgo run ./cmd/gazetteer sources catalog --json > docs/sources.json", path)
	}
}

// TestEverySourceImplementsEmptyReporter enforces the uniform-contract promise
// AGENTS.md makes ("learn one source, know all"): every registered Source's
// typed Result must implement gazetteer.EmptyReporter (IsEmpty). The framework
// uses it to record StatusOKEmpty, and every caller (and agent) relies on it —
// a new source that omits it silently breaks the contract.
func TestEverySourceImplementsEmptyReporter(t *testing.T) {
	for _, name := range gazetteer.RegisteredNames() {
		f := gazetteer.Lookup(name)
		if f == nil {
			t.Errorf("registered source %q has no result factory", name)
			continue
		}
		if _, ok := f().(gazetteer.EmptyReporter); !ok {
			t.Errorf("%s Result does not implement gazetteer.EmptyReporter (IsEmpty() bool) — breaks the uniform contract", name)
		}
	}
}

// TestBuildCatalog smokes the merge: every registered source appears once,
// name-sorted, with its descriptor wired in.
func TestBuildCatalog(t *testing.T) {
	cat := buildCatalog()
	if len(cat) != len(gazetteer.RegisteredNames()) {
		t.Fatalf("catalog has %d entries, want %d (one per registered source)", len(cat), len(gazetteer.RegisteredNames()))
	}
	for i, e := range cat {
		if i > 0 && cat[i-1].Name >= e.Name {
			t.Errorf("catalog not name-sorted at %d: %q then %q", i, cat[i-1].Name, e.Name)
		}
		if e.Summary == "" {
			t.Errorf("%s: catalog entry lost its summary", e.Name)
		}
	}
}

// TestDescriptorInputTokens validates every descriptor clause against the
// canonical input vocabulary — a typo'd or unknown token would silently
// break `query --explain`'s diagnosis, so it fails the build instead.
func TestDescriptorInputTokens(t *testing.T) {
	for name, desc := range sourceDescriptors {
		for _, c := range desc.Inputs {
			if len(c.AnyOf) == 0 {
				t.Errorf("%s: input clause with empty AnyOf", name)
			}
			for _, tok := range c.AnyOf {
				if _, ok := inputTokenPresent[tok]; !ok {
					t.Errorf("%s: input token %q is not in the canonical vocabulary (inputTokenPresent)", name, tok)
				}
			}
		}
	}
}

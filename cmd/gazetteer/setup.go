package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/httpx"

	"github.com/bpineau/gazetteer/sources/iris"
)

// commonFlags is the per-sub-command flag bundle every "real" command
// registers. Sub-commands embed it and call register* before defining
// their own flags.
type commonFlags struct {
	verbose bool
}

// registerVerbose installs --verbose. Other flags (--dump, --json, …)
// are owned by the sub-command that uses them.
func (c *commonFlags) registerVerbose(fs *flag.FlagSet) {
	fs.BoolVar(&c.verbose, "verbose", false, "Enable DEBUG-level slog output to stderr")
}

// setupLogger installs a slog handler honouring --verbose. Returns the
// resulting logger so callers may also pass it to the gazetteer
// Builder for explicit propagation.
func (c *commonFlags) setupLogger() *slog.Logger {
	lvl := slog.LevelInfo
	if c.verbose {
		lvl = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// runtimeDeps is the bundle of shared infra a sub-command needs to run
// the gazetteer lib end-to-end against live HTTP backends. Built once
// per process via newRuntimeDeps and reused across calls.
type runtimeDeps struct {
	HTTP       *httpx.Client
	BAN        *banx.BANClient
	Communes   communes.Communes
	Normalizer gazetteer.Normalizer

	// DataDir is the resolved gazetteer data directory injected into every
	// block-dataset Source so refreshed artifacts override the embedded
	// ones. Empty means embedded-only (e.g. when the user cache dir cannot
	// be resolved).
	DataDir string
}

// newRuntimeDeps builds the shared httpx.Client, the BAN client, and
// the embedded communes table, plus a BAN-backed Normalizer ready to
// be wired into gazetteer.Builder.WithNormalizer (or called directly
// via Normalizer.Normalize).
func newRuntimeDeps() (*runtimeDeps, error) {
	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		return nil, fmt.Errorf("httpx: %w", err)
	}
	ban := banx.NewBANClient(hc)
	com := communes.MustDefault()
	// A failure to resolve the user cache dir is non-fatal: block sources
	// fall back to their embedded copies when DataDir is empty.
	dataDir, _ := dataset.ResolveDir("")
	// The IRIS source doubles as the Normalizer's IRISResolver, so addresses
	// resolved by the CLI carry their IRIS code.
	norm := gazetteer.NewBANNormalizer(ban, com).WithIRIS(iris.NewSource(iris.Options{DataDir: dataDir}))
	return &runtimeDeps{HTTP: hc, BAN: ban, Communes: com, Normalizer: norm, DataDir: dataDir}, nil
}

// parseInterleaved runs fs.Parse repeatedly, harvesting positional
// arguments around mid-command flags. Go's flag package stops at the
// first non-flag token, which makes `gazetteer query '<addr>' -surface 46`
// silently treat `-surface 46` as part of the address. This helper
// re-enters Parse on the remainder after each positional, so flags
// interleaved with positional arguments work as users expect.
//
// `--` ends parsing: anything after it is taken as positional verbatim.
func parseInterleaved(fs *flag.FlagSet, argv []string) ([]string, error) {
	var positional []string
	rest := argv
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if rest[0] == "--" {
			positional = append(positional, rest[1:]...)
			return positional, nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

// parsePositional joins the remaining positional args of fs into a
// single string (typically the address). Returns errUsage when nothing
// follows the flags.
//
// Callers MUST have invoked parseInterleaved before this so flags
// after the address (e.g. `<addr> -surface 46`) are recognised
// rather than collapsed into the address text.
func parsePositional(fs *flag.FlagSet, positional []string, what string) (string, error) {
	if len(positional) == 0 {
		fmt.Fprintf(fs.Output(), "missing %s\n\n", what)
		fs.Usage()
		return "", errUsage
	}
	// Operators frequently forget to quote multi-word addresses;
	// re-join silently so `gazetteer query 10 rue de la paix 75002`
	// works as if it had been quoted.
	if len(positional) == 1 {
		return positional[0], nil
	}
	joined := positional[0]
	for _, w := range positional[1:] {
		joined += " " + w
	}
	return joined, nil
}

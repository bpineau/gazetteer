package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/bpineau/gazetteer"
	"github.com/bpineau/gazetteer/pkg/banx"
	"github.com/bpineau/gazetteer/pkg/communes"
	"github.com/bpineau/gazetteer/pkg/httpx"
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
	HTTP     *httpx.Client
	BAN      *banx.BANClient
	Communes communes.Communes
}

// newRuntimeDeps builds the shared httpx.Client, the BAN client, and
// the embedded communes table. Also installs the lib's default
// Normalizer so gazetteer.NormalizeAddress works from anywhere in the
// process — mirrors what internal/cli/root.go does in encheridor.
func newRuntimeDeps() (*runtimeDeps, error) {
	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		return nil, fmt.Errorf("httpx: %w", err)
	}
	ban := banx.NewBANClient(hc)
	com := communes.MustDefault()
	gazetteer.SetDefaultNormalizer(gazetteer.NewBANNormalizer(ban, com))
	return &runtimeDeps{HTTP: hc, BAN: ban, Communes: com}, nil
}

// parsePositional joins the remaining positional args of fs into a
// single string (typically the address). Returns errUsage when nothing
// follows the flags.
func parsePositional(fs *flag.FlagSet, what string) (string, error) {
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintf(fs.Output(), "missing %s\n\n", what)
		fs.Usage()
		return "", errUsage
	}
	// Operators frequently forget to quote multi-word addresses;
	// re-join silently so `gazetteer query 10 rue de la paix 75002`
	// works as if it had been quoted.
	if len(rest) == 1 {
		return rest[0], nil
	}
	joined := rest[0]
	for _, w := range rest[1:] {
		joined += " " + w
	}
	return joined, nil
}

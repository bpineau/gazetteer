package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bpineau/gazetteer/gazetteer"
)

// runRefresh is a v1 stub. The eventual `refresh` implementation
// re-fetches upstream data and regenerates the embedded CSV / JSON
// files shipped by sources that carry one (carteloyers, encadrement,
// filosofi, taxefonciere, vacance, osm_transit). Each source defines
// its own data URL + parser, so the full implementation is a
// per-source pull-request rather than a one-line CLI change — deferred
// to Phase 6 v2 per the design plan.
func runRefresh(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer refresh <source>|all")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Re-fetches upstream data + regenerates the embedded file(s) for")
		fmt.Fprintln(fs.Output(), "a source. v1: STUB ONLY — no per-source implementation has shipped yet.")
		fmt.Fprintln(fs.Output(), "See Phase 6 v2.")
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	target := fs.Arg(0)

	targets := []string{target}
	if target == "all" {
		targets = gazetteer.RegisteredNames()
	} else {
		// Validate the name against the lib's registry so the operator
		// gets a useful error when they typo.
		if gazetteer.Lookup(target) == nil {
			return fmt.Errorf("unknown source %q (registered: %v)", target, gazetteer.RegisteredNames())
		}
	}

	for _, name := range targets {
		fmt.Fprintf(os.Stdout, "refresh %s: not implemented (Phase 6 v1 stub; see Phase 6 v2)\n", name)
	}
	return nil
}

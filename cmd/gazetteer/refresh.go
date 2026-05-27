package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bpineau/gazetteer/gazetteer"
)

// runRefresh is a stub. The eventual `refresh` implementation re-fetches
// upstream data and regenerates the embedded CSV / JSON files shipped
// by Sources that carry one (carteloyers, encadrement, filosofi,
// taxefonciere, vacance, osm_transit). Each Source defines its own
// data URL + parser, so the full implementation lands as a per-source
// contribution rather than a one-line CLI change.
func runRefresh(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer refresh <source>|all")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Re-fetches upstream data + regenerates the embedded file(s) for")
		fmt.Fprintln(fs.Output(), "a source. STUB ONLY — no per-source implementation has shipped yet.")
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
		fmt.Fprintf(os.Stdout, "refresh %s: not implemented (per-source refresh stub)\n", name)
	}
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/bpineau/gazetteer/gazetteer"
)

// runNormalize implements `gazetteer normalize [--json] <addr>`. Calls
// the lib's BAN-backed Normalizer (via gazetteer.NormalizeAddress) and
// prints the resulting Listing.
func runNormalize(ctx context.Context, args []string) error {
	var (
		flags   commonFlags
		jsonOut bool
	)
	fs := flag.NewFlagSet("normalize", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer normalize [--json] [--verbose] <addr>")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Resolves <addr> via the BAN (api-adresse.data.gouv.fr) and prints")
		fmt.Fprintln(fs.Output(), "the canonical Listing (address, city, zip, INSEE, lat, lon).")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	flags.registerVerbose(fs)
	fs.BoolVar(&jsonOut, "json", false, "Emit the Listing as indented JSON instead of a human summary")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return errUsage
	}
	addr, err := parsePositional(fs, positional, "<addr>")
	if err != nil {
		return err
	}

	flags.setupLogger()

	deps, err := newRuntimeDeps()
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	listing, err := deps.Normalizer.Normalize(ctx, addr)
	if err != nil {
		return fmt.Errorf("normalize %q: %w", addr, err)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(listing)
	}
	printListing(os.Stdout, listing)
	return nil
}

// printListing renders the human-friendly summary for `normalize`.
// Kept package-local so query / appraise can reuse it when they print
// the resolved listing as part of their header.
func printListing(out *os.File, l gazetteer.Listing) {
	fmt.Fprintf(out, "address  %s\n", l.Address)
	if l.City != "" {
		fmt.Fprintf(out, "city     %s\n", l.City)
	}
	if l.Zip != "" {
		fmt.Fprintf(out, "zip      %s\n", l.Zip)
	}
	if l.INSEE != "" {
		fmt.Fprintf(out, "insee    %s\n", l.INSEE)
	}
	if l.Lat != nil && l.Lon != nil {
		fmt.Fprintf(out, "lat,lon  %.6f,%.6f\n", *l.Lat, *l.Lon)
	}
}

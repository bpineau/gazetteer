package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bpineau/gazetteer/appraisal/zonescore"
	"github.com/bpineau/gazetteer/gazetteer"
)

// runCompare implements `gazetteer compare [flags] <addr1> <addr2> [...]`.
// It normalises each (separately-quoted) address, collects every candidate in
// parallel, scores them with the same yield-first profile, and prints them
// ranked best-first.
func runCompare(ctx context.Context, args []string) error {
	cf, addrs, err := parseCompareFlags(args)
	if err != nil {
		return err
	}
	if len(addrs) < 2 {
		fmt.Fprintln(os.Stderr, "compare needs at least two addresses (quote each separately)")
		return errUsage
	}

	logger := cf.common.setupLogger()
	deps, err := newRuntimeDeps()
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	sources, err := resolveSources(deps, splitCSV(cf.sources))
	if err != nil {
		return err
	}
	pt, err := parsePropertyType(cf.propertyType)
	if err != nil {
		return err
	}

	builder := gazetteer.NewBuilder().
		WithHTTPClient(deps.HTTP.HTTPClient()).
		WithLogger(logger)
	for _, s := range sources {
		builder = builder.With(s)
	}
	client, err := builder.Build()
	if err != nil {
		return fmt.Errorf("build gazetteer client: %w", err)
	}

	listings := make([]gazetteer.Listing, 0, len(addrs))
	for _, a := range addrs {
		l, err := deps.Normalizer.Normalize(ctx, a)
		if err != nil {
			return fmt.Errorf("normalize %q: %w", a, err)
		}
		l.PropertyType = pt
		if cf.surface > 0 {
			s := cf.surface
			l.SurfaceM2 = &s
		}
		if cf.rooms > 0 {
			r := cf.rooms
			l.Rooms = &r
		}
		listings = append(listings, l)
	}

	if cf.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cf.timeout)
		defer cancel()
	}
	cmp := zonescore.Compare(ctx, client, listings)

	if cf.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cmp)
	}
	printComparison(os.Stdout, cmp)
	return nil
}

// parseCompareFlags reuses the query flag set but takes every positional as a
// separate address (rather than joining them into one).
func parseCompareFlags(args []string) (*queryFlags, []string, error) {
	var q queryFlags
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(),
			"Usage: gazetteer compare [--property-type ...] [--surface m²] [--rooms N] [--source ...] [--json] [--verbose] \"<addr1>\" \"<addr2>\" [...]\n")
		fmt.Fprintln(fs.Output(), "\nRanks the addresses best-first by yield-first zone score. Quote each address separately.")
	}
	q.common.registerVerbose(fs)
	fs.StringVar(&q.sources, "source", "", "Comma-separated source names (default: all). See `sources list`.")
	fs.StringVar(&q.propertyType, "property-type", "apartment", "Property type: apartment | house | land | commercial.")
	fs.Float64Var(&q.surface, "surface", 0, "Habitable surface in m².")
	fs.IntVar(&q.rooms, "rooms", 0, "Room count (1, 2, 3…).")
	fs.DurationVar(&q.timeout, "timeout", 30*time.Second, "Overall budget for the collects (0 disables).")
	fs.BoolVar(&q.jsonOut, "json", false, "Emit the full Comparison as indented JSON")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return nil, nil, errUsage
	}
	return &q, positional, nil
}

// printComparison renders the ranked candidates as a compact table plus the
// winner's axis breakdown.
func printComparison(out io.Writer, cmp zonescore.Comparison) {
	fmt.Fprintln(out, "compare (yield-first):")
	fmt.Fprintf(out, "  %-4s %-44s %7s %7s %9s %9s %s\n", "rank", "address", "score", "yield", "price/m²", "rent/m²", "conf")
	for _, e := range cmp.Entries {
		fmt.Fprintf(out, "  #%-3d %-44s %7.1f %6.1f%% %9.0f %9.1f %s\n",
			e.Rank, truncate(addrOf(e.Listing), 44), e.Score.Composite, e.YieldPct,
			e.PriceEURPerM2, e.RentEURPerM2, e.Score.Confidence.String())
	}
	if len(cmp.Entries) == 0 {
		return
	}
	w := cmp.Entries[0]
	fmt.Fprintf(out, "\n  winner: %s (%.1f / 100)\n", addrOf(w.Listing), w.Score.Composite)
	for _, a := range w.Score.Axes {
		if !a.Present {
			continue
		}
		fmt.Fprintf(out, "    %-12s %5.1f  weight=%.2f  %s\n", a.Name, a.Value, a.Weight, a.Reason)
	}
}

func addrOf(l gazetteer.Listing) string {
	if l.Address != "" {
		return l.Address
	}
	return l.INSEE
}

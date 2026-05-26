package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// runQuery implements `gazetteer query [--source ...] [--json] [--verbose]
// [--dump] <addr>`. Normalises the address, fires the selected Sources
// in parallel via the gazetteer Client, prints either a per-source
// human summary or the full Dossier as JSON.
func runQuery(ctx context.Context, args []string) error {
	q, err := parseQueryFlags("query", args)
	if err != nil {
		return err
	}
	dossier, err := executeQuery(ctx, q)
	if err != nil {
		return err
	}

	if q.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(dossier)
	}
	printDossierSummary(os.Stdout, dossier)
	return nil
}

// queryFlags is the shared flag bundle for `query` and `appraise`.
// Both sub-commands take the same set; `appraise` adds the appraisal
// synthesis on top of the same collect pipeline.
type queryFlags struct {
	common  commonFlags
	sources string
	jsonOut bool
	dump    bool
	addr    string
}

// parseQueryFlags wires the shared flag set used by `query` and
// `appraise`. The first arg is the sub-command name (for the Usage
// banner); the rest are the user's argv.
func parseQueryFlags(cmd string, args []string) (*queryFlags, error) {
	var q queryFlags
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: gazetteer %s [--source dvf,osm_transit,...] [--json] [--verbose] [--dump] <addr>\n", cmd)
		fmt.Fprintln(fs.Output())
		fmt.Fprintf(fs.Output(), "Available sources: %s\n", strings.Join(allSourceNames(), ", "))
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	q.common.registerVerbose(fs)
	fs.StringVar(&q.sources, "source", "", "Comma-separated source names (default: all officials + atomic rental). See list above.")
	fs.BoolVar(&q.jsonOut, "json", false, "Emit the full Dossier as indented JSON")
	fs.BoolVar(&q.dump, "dump", false, "Log raw HTTP request/response payloads (sources that honour it)")
	if err := fs.Parse(args); err != nil {
		return nil, errUsage
	}
	addr, err := parsePositional(fs, "<addr>")
	if err != nil {
		return nil, err
	}
	q.addr = addr
	return &q, nil
}

// executeQuery is the shared collect pipeline used by `query` and
// `appraise`. Returns the populated Dossier; sub-commands choose how
// to render it.
func executeQuery(ctx context.Context, q *queryFlags) (gazetteer.Dossier, error) {
	logger := q.common.setupLogger()

	deps, err := newRuntimeDeps()
	if err != nil {
		return gazetteer.Dossier{}, fmt.Errorf("setup: %w", err)
	}

	selected := splitCSV(q.sources)
	sources, err := resolveSources(deps, selected)
	if err != nil {
		return gazetteer.Dossier{}, err
	}

	listing, err := deps.Normalizer.Normalize(ctx, q.addr)
	if err != nil {
		return gazetteer.Dossier{}, fmt.Errorf("normalize %q: %w", q.addr, err)
	}

	builder := gazetteer.NewBuilder().
		WithHTTPClient(deps.HTTP.HTTPClient()).
		WithLogger(logger).
		WithDebugDump(q.dump)
	for _, s := range sources {
		builder = builder.With(s)
	}
	client, err := builder.Build()
	if err != nil {
		return gazetteer.Dossier{}, fmt.Errorf("build gazetteer client: %w", err)
	}

	return client.Collect(ctx, listing), nil
}

// splitCSV splits a comma-separated list and trims whitespace around
// each element. Returns nil for an empty / whitespace input so callers
// can rely on len(out) == 0 to mean "use defaults".
func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// printDossierSummary renders one line per source: status + a short,
// source-agnostic detail (err message or "ok"). Sources are printed in
// sorted name order for stable, diff-friendly output.
func printDossierSummary(out io.Writer, d gazetteer.Dossier) {
	fmt.Fprintln(out, "listing:")
	if l := d.Listing; l.Address != "" {
		fmt.Fprintf(out, "  address  %s\n", l.Address)
	}
	if d.Listing.INSEE != "" {
		fmt.Fprintf(out, "  insee    %s\n", d.Listing.INSEE)
	}
	if d.Listing.Lat != nil && d.Listing.Lon != nil {
		fmt.Fprintf(out, "  lat,lon  %.6f,%.6f\n", *d.Listing.Lat, *d.Listing.Lon)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "results:")
	names := make([]string, 0, len(d.Results))
	for n := range d.Results {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := d.Results[n]
		detail := r.Status.String()
		if r.Err != nil {
			detail += ": " + r.Err.Error()
		}
		fmt.Fprintf(out, "  %-14s v%d  %s\n", n, r.Version, detail)
	}

	if !d.StartedAt.IsZero() && !d.FinishedAt.IsZero() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "elapsed: %s\n", d.FinishedAt.Sub(d.StartedAt).Round(0))
	}
}

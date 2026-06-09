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
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

// runQuery implements `gazetteer query [--source ...] [--json] [--verbose]
// [--explain] <addr>`. Normalises the address, fires the selected
// Sources in parallel via the gazetteer Client, then prints either a
// per-source human summary, the full Dossier as JSON (--json), or a
// per-source why-empty/why-failed diagnosis (--explain).
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
	if q.explain {
		printDiagnosis(os.Stdout, dossier)
		return nil
	}
	printDossierSummary(os.Stdout, dossier)
	return nil
}

// queryFlags is the shared flag bundle for `query` and `appraise`.
// Both sub-commands take the same set; `appraise` adds the appraisal
// synthesis on top of the same collect pipeline.
type queryFlags struct {
	common       commonFlags
	sources      string
	propertyType string        // "apartment" (default) | "house" | "land" | "commercial"
	surface      float64       // m²; 0 ⇒ unset
	rooms        int           // 0 ⇒ unset
	timeout      time.Duration // overall budget for the Collect; 0 ⇒ no deadline
	jsonOut      bool
	explain      bool   // diagnose per-source why-empty/why-failed (query only)
	profile      string // ZoneScore weight preset (appraise / compare only)
	addr         string
}

// parseQueryFlags wires the shared flag set used by `query` and
// `appraise`. The first arg is the sub-command name (for the Usage
// banner); the rest are the user's argv.
func parseQueryFlags(cmd string, args []string) (*queryFlags, error) {
	var q queryFlags
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(),
			"Usage: gazetteer %s [--property-type apartment|house|land|commercial] [--surface m²] [--rooms N] [--source dvf,osm_transit,...] [--json] [--verbose] <addr>\n", cmd)
		fmt.Fprintln(fs.Output())
		fmt.Fprintf(fs.Output(), "Available sources: %s\n", strings.Join(allSourceNames(), ", "))
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	q.common.registerVerbose(fs)
	fs.StringVar(&q.sources, "source", "", "Comma-separated source names (default: all sources except opt-in ones like bdnb). See list above.")
	fs.StringVar(&q.propertyType, "property-type", "apartment",
		"Property type: apartment | house | land | commercial. Drives source eligibility (DVF, encadrement, …).")
	fs.Float64Var(&q.surface, "surface", 0,
		"Habitable surface in m² (e.g. 45). Required by DVF, taxe-foncière, encadrement for a meaningful answer.")
	fs.IntVar(&q.rooms, "rooms", 0,
		"Room count (1, 2, 3…). Required by carteloyers / encadrement / locservice for a typed rent reference.")
	fs.DurationVar(&q.timeout, "timeout", 30*time.Second,
		"Overall budget for the Collect (deadline propagated via ctx). Slow Sources past this point return ctx.DeadlineExceeded → StatusFailedTransient. 0 disables the deadline.")
	fs.BoolVar(&q.jsonOut, "json", false, "Emit the full Dossier as indented JSON")
	fs.BoolVar(&q.explain, "explain", false, "Diagnose per source WHY it returned nothing (missing input vs no data for this address)")
	fs.StringVar(&q.profile, "profile", "",
		"ZoneScore weight preset for appraise/compare: yield (default) | balanced | patrimoine | transport.")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return nil, errUsage
	}
	addr, err := parsePositional(fs, positional, "<addr>")
	if err != nil {
		return nil, err
	}
	q.addr = addr
	return &q, nil
}

// parsePropertyType maps the --property-type flag onto the
// gazetteer.PropertyType enum. Empty string defaults to apartment
// (the most common rental-investor case). Unknown values are
// rejected so the user doesn't silently get an unfiltered run.
func parsePropertyType(s string) (gazetteer.PropertyType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "apartment", "appartement", "flat":
		return gazetteer.PropertyApartment, nil
	case "house", "maison":
		return gazetteer.PropertyHouse, nil
	case "land", "terrain":
		return gazetteer.PropertyLand, nil
	case "commercial", "commerce", "local":
		return gazetteer.PropertyCommercial, nil
	default:
		return "", fmt.Errorf("unknown --property-type %q (want apartment | house | land | commercial)", s)
	}
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

	// Layer the user-supplied property attributes on top of the
	// normalised Listing so sources that gate on them (DVF, encadrement,
	// taxefonciere, carteloyers, locservice, …) can produce a useful
	// answer.
	pt, err := parsePropertyType(q.propertyType)
	if err != nil {
		return gazetteer.Dossier{}, err
	}
	listing.PropertyType = pt
	if q.surface > 0 {
		s := q.surface
		listing.SurfaceM2 = &s
	}
	if q.rooms > 0 {
		r := q.rooms
		listing.Rooms = &r
	}

	builder := gazetteer.NewBuilder().
		WithHTTPClient(deps.HTTP.HTTPClient()).
		WithLogger(logger)
	for _, s := range sources {
		builder = builder.With(s)
	}
	client, err := builder.Build()
	if err != nil {
		return gazetteer.Dossier{}, fmt.Errorf("build gazetteer client: %w", err)
	}

	if q.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, q.timeout)
		defer cancel()
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

// printDossierSummary renders the Listing context and one block per
// Source: a header line (name, version, status, one-line headline
// extracted by the source-specific renderer) plus any extra detail
// lines (indented). Sources are printed in sorted name order for
// stable, diff-friendly output.
func printDossierSummary(out io.Writer, d gazetteer.Dossier) {
	fmt.Fprintln(out, "listing:")
	if l := d.Listing; l.Address != "" {
		fmt.Fprintf(out, "  address  %s\n", l.Address)
	}
	if d.Listing.City != "" {
		fmt.Fprintf(out, "  city     %s\n", d.Listing.City)
	}
	if d.Listing.Zip != "" {
		fmt.Fprintf(out, "  postcode %s\n", d.Listing.Zip)
	}
	if d.Listing.INSEE != "" {
		fmt.Fprintf(out, "  insee    %s\n", d.Listing.INSEE)
	}
	if d.Listing.Lat != nil && d.Listing.Lon != nil {
		fmt.Fprintf(out, "  lat,lon  %.6f,%.6f\n", *d.Listing.Lat, *d.Listing.Lon)
	}
	if d.Listing.PropertyType != "" {
		fmt.Fprintf(out, "  type     %s\n", d.Listing.PropertyType)
	}
	if d.Listing.SurfaceM2 != nil {
		fmt.Fprintf(out, "  surface  %.0f m²\n", *d.Listing.SurfaceM2)
	}
	if d.Listing.Rooms != nil {
		fmt.Fprintf(out, "  rooms    %d\n", *d.Listing.Rooms)
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
		headline, extra := summariseResult(n, r)
		fmt.Fprintf(out, "  %-14s v%d  %-9s  %s\n",
			n, r.Version, abbreviateStatus(r.Status), headline)
		for _, line := range extra {
			fmt.Fprintf(out, "                          %s\n", line)
		}
	}

	// Surface the opt-in Sources the default run does not exercise so
	// the operator sees what's NOT in the table rather than wondering
	// why osm_transit / bdnb are missing.
	if optedOut := optedOutSources(d); len(optedOut) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "opt-in (skipped by default; pass via --source): %s\n",
			strings.Join(optedOut, ", "))
	}

	if !d.StartedAt.IsZero() && !d.FinishedAt.IsZero() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "elapsed: %s\n", d.FinishedAt.Sub(d.StartedAt).Round(0))
	}
}

// optedOutSources returns the names of every Source the CLI knows but
// that was NOT exercised in d (i.e. either Default=false in the
// registry or absent from the user-supplied --source list). Stable
// alphabetic order so the footer is diff-friendly.
func optedOutSources(d gazetteer.Dossier) []string {
	have := make(map[string]struct{}, len(d.Results))
	for n := range d.Results {
		have[n] = struct{}{}
	}
	all := allSourceNames()
	var out []string
	for _, n := range all {
		if _, ok := have[n]; !ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

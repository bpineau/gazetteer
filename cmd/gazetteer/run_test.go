package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/appraisal/zonescore"
	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// capture is the test-side pair of streams: it drives a whole sub-command
// against buffers instead of the process's own stdout/stderr, so a test
// asserts on exactly what the user would see (and on which stream).
type capture struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func (c *capture) streams() streams { return streams{out: &c.stdout, err: &c.stderr} }

// discardStreams is the pair for calls whose output is not under test.
func discardStreams() streams { return streams{out: io.Discard, err: io.Discard} }

// TestRunDispatch drives the real entry point, `run`, over every
// network-free sub-command: the dispatch table, the help texts, the flag
// parsing and the error/exit contract main maps to a status code (errUsage =
// "the command already printed a banner", any other error = printed as
// "gazetteer: <err>", nil = exit 0).
//
// Nothing here touches the network: the commands listed either read the
// embedded catalog/datasets or fail on their arguments before the first HTTP
// round-trip.
func TestRunDispatch(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// wantUsage asserts errors.Is(err, errUsage) — the "printed its own
		// banner, exit 1" contract. wantErr asserts a substring of a plain
		// error (which main prefixes with "gazetteer:"). Both empty = success.
		wantUsage bool
		wantErr   string
		stdoutHas []string
		stderrHas []string
		stdoutNot []string
	}{
		{
			name:      "version",
			args:      []string{"version"},
			stdoutHas: []string{"gazetteer ", "module  github.com/bpineau/gazetteer"},
		},
		{
			name:      "version_via_dash_v",
			args:      []string{"-v"},
			stdoutHas: []string{"gazetteer "},
		},
		{
			name:      "version_rejects_unknown_flag",
			args:      []string{"version", "--bogus"},
			wantUsage: true,
			stderrHas: []string{"flag provided but not defined: -bogus"},
		},
		{
			name:      "help_goes_to_stdout",
			args:      []string{"help"},
			stdoutHas: []string{"Usage: gazetteer <command>", "query", "appraise", "compare", "refresh"},
		},
		{
			name:      "no_args_is_a_usage_error_on_stderr",
			args:      nil,
			wantUsage: true,
			stderrHas: []string{"Usage: gazetteer <command>"},
			stdoutNot: []string{"Usage"},
		},
		{
			name:      "unknown_command",
			args:      []string{"banana"},
			wantUsage: true,
			stderrHas: []string{`unknown command "banana"`, "Usage: gazetteer <command>"},
		},
		{
			name:      "sources_without_subcommand",
			args:      []string{"sources"},
			wantUsage: true,
			stderrHas: []string{"gazetteer sources list", "gazetteer sources catalog"},
		},
		{
			name:      "sources_help_goes_to_stdout",
			args:      []string{"sources", "help"},
			stdoutHas: []string{"gazetteer sources dimensions"},
		},
		{
			name:      "sources_unknown_subcommand",
			args:      []string{"sources", "bogus"},
			wantUsage: true,
			stderrHas: []string{`unknown sources sub-command "bogus"`},
		},
		{
			name:      "sources_list",
			args:      []string{"sources", "list"},
			stdoutHas: []string{"dvf", "carteloyers", "(opt-in via --source)"},
		},
		{
			name:      "sources_list_takes_no_argument",
			args:      []string{"sources", "list", "dvf"},
			wantUsage: true,
			stderrHas: []string{"Usage: gazetteer sources list"},
		},
		{
			name:      "sources_doc_emits_the_result_shape",
			args:      []string{"sources", "doc", "carteloyers"},
			stdoutHas: []string{"loyer_med_eur_per_m2_cc"},
		},
		{
			name:    "sources_doc_unknown_source_is_a_plain_error",
			args:    []string{"sources", "doc", "nope"},
			wantErr: `unknown source "nope"`,
		},
		{
			name:      "sources_doc_needs_exactly_one_name",
			args:      []string{"sources", "doc"},
			wantUsage: true,
			stderrHas: []string{"Usage: gazetteer sources doc <name>"},
		},
		{
			name:      "sources_catalog_text",
			args:      []string{"sources", "catalog"},
			stdoutHas: []string{"inputs:", "coverage:", "dvf"},
		},
		{
			name:      "sources_catalog_json",
			args:      []string{"sources", "catalog", "--json"},
			stdoutHas: []string{`"result_schema"`, `"name": "dvf"`},
		},
		{
			name:      "sources_dimensions",
			args:      []string{"sources", "dimensions"},
			stdoutHas: []string{"Loyers", "Risques & nuisances", "carteloyers"},
		},
		{
			name:    "sources_dimensions_takes_no_argument",
			args:    []string{"sources", "dimensions", "loyers"},
			wantErr: "takes no arguments",
		},
		{
			name:      "refresh_list_reports_dataset_state",
			args:      []string{"refresh", "--list"},
			stdoutHas: []string{"datadir:", "SOURCE", "REFRESHABLE", "delinquance"},
		},
		{
			name:    "refresh_rejects_a_non_dataset_source",
			args:    []string{"refresh", "dvf"},
			wantErr: `unknown dataset source "dvf"`,
		},
		{
			name:      "refresh_rejects_an_unknown_flag",
			args:      []string{"refresh", "--bogus"},
			wantUsage: true,
			stderrHas: []string{"Usage: gazetteer refresh"},
		},
		{
			name:      "query_without_address",
			args:      []string{"query"},
			wantUsage: true,
			stderrHas: []string{"missing <addr>", "Available sources:"},
		},
		{
			name:      "appraise_without_address",
			args:      []string{"appraise"},
			wantUsage: true,
			stderrHas: []string{"missing <addr>"},
		},
		{
			name:      "normalize_without_address",
			args:      []string{"normalize"},
			wantUsage: true,
			stderrHas: []string{"missing <addr>"},
		},
		{
			name:      "compare_needs_two_addresses",
			args:      []string{"compare", "12 rue X, Paris"},
			wantUsage: true,
			stderrHas: []string{"at least two addresses"},
		},
		{
			// --profile is validated BEFORE any network work, so this is a
			// pure-argument failure even though `compare` is a live command.
			name:      "compare_rejects_an_unknown_profile_before_any_fetch",
			args:      []string{"compare", "--profile", "bogus", "a", "b"},
			wantUsage: true,
			stderrHas: []string{`unknown --profile "bogus"`, "yield"},
		},
		{
			name:      "appraise_rejects_an_unknown_profile_before_any_fetch",
			args:      []string{"appraise", "--profile", "bogus", "12 rue X, Paris"},
			wantUsage: true,
			stderrHas: []string{`unknown --profile "bogus"`},
		},
		{
			// The --source list is resolved against the catalog before the
			// address is geocoded, so a typo costs no round-trip.
			name:    "query_rejects_an_unknown_source_before_any_fetch",
			args:    []string{"query", "--source", "nosuchsource", "12 rue X, Paris"},
			wantErr: `unknown source "nosuchsource"`,
		},
		{
			// Regression: --property-type used to be parsed AFTER the BAN
			// round-trip, so a typo was reported only once geocoding had
			// succeeded (and was hidden behind the normalize error when it
			// had not).
			name:    "query_rejects_an_unknown_property_type_before_any_fetch",
			args:    []string{"query", "--property-type", "castle", "12 rue X, Paris"},
			wantErr: `unknown --property-type "castle"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c capture
			err := run(context.Background(), tc.args, c.streams())

			switch {
			case tc.wantUsage:
				if !errors.Is(err, errUsage) {
					t.Errorf("err = %v, want errUsage", err)
				}
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want one containing %q", err, tc.wantErr)
				}
				if errors.Is(err, errUsage) {
					t.Error("err is errUsage; a plain error is printed by main, a usage one is not")
				}
			default:
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			}

			for _, want := range tc.stdoutHas {
				if !strings.Contains(c.stdout.String(), want) {
					t.Errorf("stdout missing %q:\n%s", want, c.stdout.String())
				}
			}
			for _, want := range tc.stderrHas {
				if !strings.Contains(c.stderr.String(), want) {
					t.Errorf("stderr missing %q:\n%s", want, c.stderr.String())
				}
			}
			for _, unwanted := range tc.stdoutNot {
				if strings.Contains(c.stdout.String(), unwanted) {
					t.Errorf("stdout should not carry %q:\n%s", unwanted, c.stdout.String())
				}
			}
		})
	}
}

// TestRunSourcesCatalogJSONIsParseable pins the machine-readable contract
// AGENTS.md points agents at: `sources catalog --json` must be one JSON
// array, each entry naming a source and its Result schema.
func TestRunSourcesCatalogJSONIsParseable(t *testing.T) {
	var c capture
	if err := run(context.Background(), []string{"sources", "catalog", "--json"}, c.streams()); err != nil {
		t.Fatalf("sources catalog --json: %v", err)
	}
	var entries []struct {
		Name         string          `json:"name"`
		Version      int             `json:"version"`
		ResultSchema json.RawMessage `json:"result_schema"`
	}
	if err := json.Unmarshal(c.stdout.Bytes(), &entries); err != nil {
		t.Fatalf("catalog is not valid JSON: %v", err)
	}
	if len(entries) != len(allSourceNames()) {
		t.Errorf("catalog has %d entries, want one per source (%d)", len(entries), len(allSourceNames()))
	}
	for _, e := range entries {
		if e.Name == "" || e.Version == 0 {
			t.Errorf("catalog entry %+v is missing its name/version", e)
		}
	}
}

// TestRunSourcesDocIsTheRegisteredResult checks `sources doc` renders the
// registry's typed Result rather than some ad hoc shape: the emitted JSON
// must round-trip into the very type the registry hands out.
func TestRunSourcesDocIsTheRegisteredResult(t *testing.T) {
	for _, name := range gazetteer.RegisteredNames() {
		var c capture
		if err := run(context.Background(), []string{"sources", "doc", name}, c.streams()); err != nil {
			t.Fatalf("sources doc %s: %v", name, err)
		}
		if err := json.Unmarshal(c.stdout.Bytes(), gazetteer.Lookup(name)()); err != nil {
			t.Errorf("sources doc %s: output does not round-trip into the Result: %v", name, err)
		}
	}
}

// TestJSONAndExplainCoexist pins the --json / --explain precedence: both are
// accepted together and --json wins (the full Dossier it emits carries every
// Status the diagnosis reads, so nothing is lost).
func TestJSONAndExplainCoexist(t *testing.T) {
	q, err := parseQueryFlags("query", []string{"--json", "--explain", "12 rue X, Paris"}, discardStreams())
	if err != nil {
		t.Fatalf("parseQueryFlags: %v", err)
	}
	if !q.jsonOut || !q.explain {
		t.Fatalf("both flags must be captured: %+v", q)
	}
}

// TestTimeoutFlag covers the --timeout parsing contract on both flag sets:
// a duration string, the documented 30s default, and 0 = "no deadline".
func TestTimeoutFlag(t *testing.T) {
	q, err := parseQueryFlags("query", []string{"--timeout", "1500ms", "12 rue X"}, discardStreams())
	if err != nil {
		t.Fatalf("query --timeout: %v", err)
	}
	if q.timeout.Milliseconds() != 1500 {
		t.Errorf("query timeout = %v, want 1.5s", q.timeout)
	}

	if q, err = parseQueryFlags("query", []string{"--timeout", "0", "12 rue X"}, discardStreams()); err != nil {
		t.Fatalf("query --timeout 0: %v", err)
	}
	if q.timeout != 0 {
		t.Errorf("query timeout = %v, want 0 (deadline disabled)", q.timeout)
	}

	if _, err = parseQueryFlags("query", []string{"--timeout", "soon", "12 rue X"}, discardStreams()); !errors.Is(err, errUsage) {
		t.Errorf("--timeout soon err = %v, want errUsage", err)
	}

	cf, _, err := parseCompareFlags([]string{"--timeout", "2m", "a", "b"}, discardStreams())
	if err != nil {
		t.Fatalf("compare --timeout: %v", err)
	}
	if cf.timeout.Minutes() != 2 {
		t.Errorf("compare timeout = %v, want 2m", cf.timeout)
	}
}

// TestProfileLabel pins the fix behind printZoneScore / printComparison: the
// header must name the preset the score was actually computed with.
func TestProfileLabel(t *testing.T) {
	cases := map[string]string{
		"":           "yield-first",
		"yield":      "yield-first",
		"balanced":   "balanced",
		"patrimoine": "patrimoine",
		"transport":  "transport",
	}
	for in, want := range cases {
		if got := profileLabel(in); got != want {
			t.Errorf("profileLabel(%q) = %q, want %q", in, got, want)
		}
	}
	// Every persona the CLI accepts must have a label.
	for _, name := range zonescore.ProfileNames() {
		if profileLabel(name) == "" {
			t.Errorf("profileLabel(%q) is empty", name)
		}
	}
}

// TestZonescoreOptions covers the --profile resolution: unset = the library
// default (no Options), a known preset = its weights, an unknown one = a
// usage error naming the valid presets on stderr.
func TestZonescoreOptions(t *testing.T) {
	var c capture
	if opts, err := (&queryFlags{}).zonescoreOptions(c.streams()); err != nil || opts != nil {
		t.Errorf("unset --profile = (%v, %v), want (nil, nil)", opts, err)
	}

	opts, err := (&queryFlags{profile: "patrimoine"}).zonescoreOptions(c.streams())
	if err != nil {
		t.Fatalf("--profile patrimoine: %v", err)
	}
	if len(opts) != 1 || len(opts[0].Weights) == 0 {
		t.Fatalf("--profile patrimoine produced no weights: %+v", opts)
	}
	want, _ := zonescore.WeightsForProfile("patrimoine")
	for axis, w := range want {
		if opts[0].Weights[axis] != w {
			t.Errorf("weight[%s] = %v, want %v", axis, opts[0].Weights[axis], w)
		}
	}

	if _, err := (&queryFlags{profile: "bogus"}).zonescoreOptions(c.streams()); !errors.Is(err, errUsage) {
		t.Errorf("--profile bogus err = %v, want errUsage", err)
	}
	if got := c.stderr.String(); !strings.Contains(got, `unknown --profile "bogus"`) {
		t.Errorf("stderr = %q, want it to name the offending profile", got)
	}
	for _, name := range zonescore.ProfileNames() {
		if !strings.Contains(c.stderr.String(), name) {
			t.Errorf("the --profile error does not list %q:\n%s", name, c.stderr.String())
		}
	}
}

// TestPrintAppraisal renders the consolidated block both ways: with
// contributors (including an excluded one, which must be shown as rejected
// rather than silently dropped) and with none.
func TestPrintAppraisal(t *testing.T) {
	price := appraisal.PriceConsolidated{
		EurPerM2Cents: 812_345,
		Confidence:    appraisal.ConfidenceHigh,
		Inputs: []appraisal.PriceInput{
			{Source: "dvf", Weight: 0.6, Estimate: appraisal.PriceEstimate{EurPerM2Cents: 800_000}},
			{Source: "dvfagg", Weight: 0.4, Estimate: appraisal.PriceEstimate{EurPerM2Cents: 1_500_000},
				Excluded: true, ExcludedWhy: "outlier_z_score"},
		},
	}
	rent := appraisal.RentConsolidated{
		EurPerM2Cents: 2_150,
		Confidence:    appraisal.ConfidenceMedium,
		Bracket:       "encadrement_paris_zone_3",
		Inputs: []appraisal.RentInput{
			{Source: "oll", Weight: 1, Estimate: appraisal.RentEstimate{EurPerM2Cents: 2_150}},
		},
	}
	hazard := appraisal.HazardConsolidated{
		Confidence:      appraisal.ConfidenceLow,
		NaturalRisks:    []string{"inondation", "retrait-gonflement"},
		IndustrialRisks: []string{"icpe"},
		Inputs:          []appraisal.HazardInput{{Source: "georisques"}},
	}

	var buf bytes.Buffer
	printAppraisal(&buf, price, rent, hazard)
	out := buf.String()
	for _, want := range []string{
		"eur_per_m2     8123.45", "confidence=high", "2 input(s)",
		"dvf", "weight=0.60", "est=8000.00",
		"EXCLUDED: outlier_z_score",
		"eur_per_m2_mo  21.50", "bracket=encadrement_paris_zone_3",
		"inondation, retrait-gonflement", "icpe",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("appraisal output missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	printAppraisal(&buf, appraisal.PriceConsolidated{}, appraisal.RentConsolidated{}, appraisal.HazardConsolidated{})
	if got := strings.Count(buf.String(), "(no contributing sources)"); got != 3 {
		t.Errorf("empty appraisal names %d empty blocks, want 3:\n%s", got, buf.String())
	}
}

// TestPrintZoneScoreNamesTheActiveProfile is the regression test for the
// mis-labelled header: printZoneScore hard-coded "yield-first" whatever
// --profile had asked for, so `appraise --profile patrimoine` scored one
// thesis and announced another.
func TestPrintZoneScoreNamesTheActiveProfile(t *testing.T) {
	score := zonescore.Score{
		Composite:  61.5,
		Confidence: appraisal.ConfidenceMedium,
		Axes: []zonescore.Axis{
			{Name: "rendement", Value: 72.5, Weight: 0.35, Present: true, Reason: "yield 6.1 %"},
			{Name: "acces", Weight: 0.1},
		},
	}

	var buf bytes.Buffer
	printZoneScore(&buf, score, profileLabel(""))
	if got := buf.String(); !strings.Contains(got, "zone_score (yield-first):") {
		t.Errorf("default header = %q, want the yield-first label", got)
	}

	buf.Reset()
	printZoneScore(&buf, score, profileLabel("patrimoine"))
	out := buf.String()
	if !strings.Contains(out, "zone_score (patrimoine):") {
		t.Errorf("header does not name the active profile:\n%s", out)
	}
	if strings.Contains(out, "yield-first") {
		t.Errorf("header still claims yield-first under --profile patrimoine:\n%s", out)
	}
	for _, want := range []string{"composite      61.5 / 100", "confidence=medium", "yield 6.1 %", "acces        (n/a)"} {
		if !strings.Contains(out, want) {
			t.Errorf("zone score output missing %q:\n%s", want, out)
		}
	}
}

// TestPrintComparison covers the ranked table, the winner breakdown (present
// axes only), the profile label, and the empty comparison, which must print
// the header and no winner block.
func TestPrintComparison(t *testing.T) {
	cmp := zonescore.Comparison{Entries: []zonescore.ComparisonEntry{
		{
			Rank:          1,
			Listing:       gazetteer.Listing{Address: "12 rue X 93100 Montreuil"},
			YieldPct:      6.4,
			PriceEURPerM2: 5100,
			RentEURPerM2:  21.5,
			Score: zonescore.Score{
				Composite: 71.2, Confidence: appraisal.ConfidenceHigh,
				Axes: []zonescore.Axis{
					{Name: "rendement", Value: 80, Weight: 0.35, Present: true, Reason: "yield 6.4 %"},
					{Name: "fiscalite", Weight: 0.05},
				},
			},
		},
		{
			Rank:    2,
			Listing: gazetteer.Listing{INSEE: "75114"},
			Score:   zonescore.Score{Composite: 44.1, Confidence: appraisal.ConfidenceLow},
		},
	}}

	var buf bytes.Buffer
	printComparison(&buf, cmp, profileLabel("transport"))
	out := buf.String()
	for _, want := range []string{
		"compare (transport):", "#1", "12 rue X 93100 Montreuil", "71.2", "6.4%", "5100", "21.5", "high",
		"#2", "75114", "winner: 12 rue X 93100 Montreuil (71.2 / 100)", "rendement", "yield 6.4 %",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison output missing %q:\n%s", want, out)
		}
	}
	// Absent axes are skipped in the winner breakdown (unlike printZoneScore,
	// which lists them as (n/a)).
	if strings.Contains(out, "fiscalite") {
		t.Errorf("the winner breakdown lists an absent axis:\n%s", out)
	}

	buf.Reset()
	printComparison(&buf, zonescore.Comparison{}, profileLabel(""))
	if got := buf.String(); !strings.Contains(got, "compare (yield-first):") || strings.Contains(got, "winner") {
		t.Errorf("empty comparison = %q, want the header and no winner block", got)
	}
}

// TestPrintListing pins `normalize`'s human output, including the optional
// lines it must skip when the BAN answered without them.
func TestPrintListing(t *testing.T) {
	lat, lon := 48.856164, 2.351548
	var buf bytes.Buffer
	printListing(&buf, gazetteer.Listing{
		Address: "1 Rue de Rivoli 75001 Paris", City: "Paris", Zip: "75001",
		INSEE: "75101", Lat: &lat, Lon: &lon,
	})
	for _, want := range []string{"address  1 Rue de Rivoli", "city     Paris", "zip      75001", "insee    75101", "lat,lon  48.856164,2.351548"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("listing output missing %q:\n%s", want, buf.String())
		}
	}

	buf.Reset()
	printListing(&buf, gazetteer.Listing{Address: "somewhere"})
	if got := strings.TrimSpace(buf.String()); got != "address  somewhere" {
		t.Errorf("bare listing = %q, want the address line alone", got)
	}
}

// TestPrintReport covers the three per-dataset outcomes `refresh` reports.
func TestPrintReport(t *testing.T) {
	var buf bytes.Buffer
	printReport(&buf, dataset.Report{
		{Source: "delinquance", Processed: "delinquance.json", Bytes: 2048},
		{Source: "qpv", Processed: "qpv.json", Skipped: true, Reason: "already current"},
		{Source: "oll", Processed: "oll.json", Err: errors.New("upstream 503")},
	})
	out := buf.String()
	for _, want := range []string{"delinquance", "2.0KiB", "skipped", "already current", "FAILED", "upstream 503"} {
		if !strings.Contains(out, want) {
			t.Errorf("refresh report missing %q:\n%s", want, out)
		}
	}
}

// TestResolveSources covers the --source selection policy: unset yields the
// Default-tagged roster (opt-in sources excluded), an explicit list yields
// exactly it, and an unknown name is an error listing what exists.
func TestResolveSources(t *testing.T) {
	deps, err := newRuntimeDeps()
	if err != nil {
		t.Fatalf("newRuntimeDeps: %v", err)
	}

	defaults, err := resolveSources(deps, nil)
	if err != nil {
		t.Fatalf("resolveSources(nil): %v", err)
	}
	names := map[string]bool{}
	for _, s := range defaults {
		names[s.Name()] = true
	}
	for _, f := range sourceCatalog() {
		if got := names[f.Name]; got != f.Default {
			t.Errorf("source %q selected=%v, want %v (its Default policy)", f.Name, got, f.Default)
		}
	}

	picked, err := resolveSources(deps, []string{"dvf", "carteloyers"})
	if err != nil {
		t.Fatalf("resolveSources(explicit): %v", err)
	}
	if len(picked) != 2 || picked[0].Name() != "dvf" || picked[1].Name() != "carteloyers" {
		t.Errorf("explicit selection = %v, want [dvf carteloyers] in order", picked)
	}

	if _, err := resolveSources(deps, []string{"dvf", "nope"}); err == nil ||
		!strings.Contains(err.Error(), `unknown source "nope"`) {
		t.Errorf("unknown source err = %v, want it to name the typo and the alternatives", err)
	}
}

// TestSelectSourcesAndFlattenSets covers `refresh`'s positional-argument
// resolution and the deterministic Set ordering the report depends on.
func TestSelectSourcesAndFlattenSets(t *testing.T) {
	bySource := map[string][]dataset.Set{
		"qpv":         {{Source: "qpv", Processed: dataset.File{Name: "qpv.json"}}},
		"delinquance": {{Source: "delinquance", Processed: dataset.File{Name: "d.json"}}},
	}
	all := []string{"delinquance", "qpv"}

	for _, args := range [][]string{nil, {"all"}} {
		got, err := selectSources(bySource, all, args)
		if err != nil {
			t.Fatalf("selectSources(%v): %v", args, err)
		}
		if strings.Join(got, ",") != "delinquance,qpv" {
			t.Errorf("selectSources(%v) = %v, want every dataset source", args, got)
		}
	}

	got, err := selectSources(bySource, all, []string{"qpv"})
	if err != nil || strings.Join(got, ",") != "qpv" {
		t.Errorf("selectSources(qpv) = (%v, %v), want just qpv", got, err)
	}

	if _, err := selectSources(bySource, all, []string{"nope"}); err == nil ||
		!strings.Contains(err.Error(), `unknown dataset source "nope"`) {
		t.Errorf("err = %v, want it to name the unknown dataset source", err)
	}

	// flattenSets sorts by source name so the output is diff-friendly
	// whatever order the user named them in.
	sets := flattenSets(bySource, []string{"qpv", "delinquance"})
	if len(sets) != 2 || sets[0].Source != "delinquance" || sets[1].Source != "qpv" {
		t.Errorf("flattenSets = %v, want source-sorted", sets)
	}
}

// TestSetupLoggerHonoursVerbose checks --verbose reaches the slog handler and
// that the handler writes to the CLI's stderr, not to the process's.
func TestSetupLoggerHonoursVerbose(t *testing.T) {
	var quiet, loud bytes.Buffer
	(&commonFlags{}).setupLogger(&quiet).Debug("hidden")
	(&commonFlags{verbose: true}).setupLogger(&loud).Debug("shown")

	if quiet.Len() != 0 {
		t.Errorf("default level emitted a DEBUG line: %q", quiet.String())
	}
	if !strings.Contains(loud.String(), "shown") {
		t.Errorf("--verbose did not emit the DEBUG line: %q", loud.String())
	}
}

// TestParsePositionalJoinsUnquotedAddress covers the two escape hatches of
// the positional parser: an unquoted multi-word address is re-joined, and
// `--` hands the rest through verbatim (so an address starting with a dash
// is not read as a flag).
func TestParsePositionalJoinsUnquotedAddress(t *testing.T) {
	q, err := parseQueryFlags("query", []string{"10", "rue", "de", "la", "paix", "75002"}, discardStreams())
	if err != nil {
		t.Fatalf("unquoted address: %v", err)
	}
	if q.addr != "10 rue de la paix 75002" {
		t.Errorf("addr = %q, want the re-joined address", q.addr)
	}

	q, err = parseQueryFlags("query", []string{"--rooms", "2", "--", "-4 rue X"}, discardStreams())
	if err != nil {
		t.Fatalf("-- terminator: %v", err)
	}
	if q.addr != "-4 rue X" || q.rooms != 2 {
		t.Errorf("addr = %q, rooms = %d, want the verbatim address after --", q.addr, q.rooms)
	}

	// compare keeps each positional separate rather than joining them.
	_, addrs, err := parseCompareFlags([]string{"addr one", "--rooms", "3", "addr two"}, discardStreams())
	if err != nil {
		t.Fatalf("compare positional: %v", err)
	}
	if len(addrs) != 2 || addrs[0] != "addr one" || addrs[1] != "addr two" {
		t.Errorf("addrs = %v, want two separate candidates", addrs)
	}
}

// TestRelTo covers --go-embed-update's path label: relative to the module
// root when possible, the absolute path otherwise.
func TestRelTo(t *testing.T) {
	if got := relTo("/a/b", "/a/b/sources/qpv/data/qpv.json"); got != "sources/qpv/data/qpv.json" {
		t.Errorf("relTo = %q, want the module-relative path", got)
	}
	if got := relTo("", "sources/qpv/data/qpv.json"); got != "sources/qpv/data/qpv.json" {
		t.Errorf("relTo = %q, want the path unchanged", got)
	}
}

// TestFileSize covers the datadir probe listDatasets uses: a real file
// reports its size, a directory and a missing path report absent.
func TestFileSize(t *testing.T) {
	dir := t.TempDir()
	if _, ok := fileSize(dir); ok {
		t.Error("a directory must not report a size")
	}
	if _, ok := fileSize(filepath.Join(dir, "nope")); ok {
		t.Error("a missing file must not report a size")
	}
	p := filepath.Join(dir, "x.json")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if n, ok := fileSize(p); !ok || n != 3 {
		t.Errorf("fileSize = (%d, %v), want (3, true)", n, ok)
	}
}

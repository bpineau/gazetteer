package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// runRefresh implements `gazetteer refresh`: download each selected
// block-dataset's raw upstream input(s), rebuild its processed artifact,
// and persist both into the datadir (default ~/.cache/gazetteer). It is a
// thin front over dataset.Refresh.
//
//	gazetteer refresh [sources...|all] [flags]
//	  --data-dir DIR      target datadir (default $GAZETTEER_DATA_DIR or ~/.cache/gazetteer)
//	  --force             re-download raw even if already present
//	  --go-embed-update   also copy the rebuilt artifact into sources/<name>/data/ for re-commit
//	  --list              report per-source state and exit (no download)
func runRefresh(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		dataDir     string
		force       bool
		embedUpdate bool
		list        bool
	)
	fs.StringVar(&dataDir, "data-dir", "", "Target datadir (default $GAZETTEER_DATA_DIR or ~/.cache/gazetteer)")
	fs.BoolVar(&force, "force", false, "Rebuild even when the artifact is already present (re-download + re-transform)")
	fs.BoolVar(&embedUpdate, "go-embed-update", false, "Copy rebuilt artifacts into sources/<name>/data/ for re-commit")
	fs.BoolVar(&list, "list", false, "Report per-source dataset state and exit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer refresh [sources...|all] [flags]")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Download + rebuild block-dataset artifacts into the datadir.")
		fmt.Fprintln(fs.Output(), "With no source named (or 'all'), every dataset source is selected.")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	dir, err := dataset.ResolveDir(dataDir)
	if err != nil {
		return fmt.Errorf("resolve datadir: %w", err)
	}

	deps, err := newRuntimeDeps()
	if err != nil {
		return err
	}

	bySource, names := collectDatasetSources(deps)
	if len(bySource) == 0 {
		return fmt.Errorf("no dataset sources registered")
	}

	selected, err := selectSources(bySource, names, fs.Args())
	if err != nil {
		return err
	}

	if list {
		return listDatasets(os.Stdout, bySource, selected, dir)
	}

	sets := flattenSets(bySource, selected)
	logEvent := func(ev dataset.Event) {
		if ev.Err != nil {
			fmt.Fprintf(os.Stderr, "  %-16s %-9s %s: %v\n", ev.Source, ev.Phase, ev.File, ev.Err)
			return
		}
		fmt.Fprintf(os.Stderr, "  %-16s %-9s %s\n", ev.Source, ev.Phase, ev.File)
	}

	fmt.Fprintf(os.Stdout, "refresh: datadir %s\n", dir)
	report, refreshErr := dataset.Refresh(ctx, deps.HTTP, sets, dataset.RefreshOptions{
		Dir:   dir,
		Force: force,
		Log:   logEvent,
	})
	printReport(os.Stdout, report)

	if embedUpdate {
		if err := copyToEmbed(report, dir); err != nil {
			return err
		}
	}
	return refreshErr
}

// collectDatasetSources instantiates every source the CLI knows and keeps
// those that expose datasets, grouping their Sets by source name. Sources
// whose constructor fails (e.g. osm without a catalog) are skipped — they
// are not dataset sources anyway.
func collectDatasetSources(deps *runtimeDeps) (map[string][]dataset.Set, []string) {
	bySource := map[string][]dataset.Set{}
	for _, f := range sourceCatalog() {
		src, err := f.Build(deps)
		if err != nil {
			continue
		}
		dp, ok := src.(gazetteer.DatasetProvider)
		if !ok {
			continue
		}
		for _, s := range dp.Datasets() {
			bySource[s.Source] = append(bySource[s.Source], s)
		}
	}
	names := make([]string, 0, len(bySource))
	for n := range bySource {
		names = append(names, n)
	}
	sort.Strings(names)
	return bySource, names
}

// selectSources resolves the positional arguments into the set of source
// names to act on. No argument (or the single token "all") selects every
// dataset source. Unknown names are an error listing the valid ones.
func selectSources(bySource map[string][]dataset.Set, all []string, args []string) ([]string, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "all") {
		return all, nil
	}
	var out []string
	for _, name := range args {
		if _, ok := bySource[name]; !ok {
			return nil, fmt.Errorf("unknown dataset source %q (known: %s)", name, strings.Join(all, ", "))
		}
		out = append(out, name)
	}
	return out, nil
}

// flattenSets returns the Sets of the selected sources, source-sorted then
// in declaration order, so output is deterministic.
func flattenSets(bySource map[string][]dataset.Set, selected []string) []dataset.Set {
	sorted := append([]string(nil), selected...)
	sort.Strings(sorted)
	var sets []dataset.Set
	for _, name := range sorted {
		sets = append(sets, bySource[name]...)
	}
	return sets
}

// listDatasets prints, per selected dataset, where Open would resolve it
// from (datadir / embed / none) and the datadir copy's size when present.
func listDatasets(w io.Writer, bySource map[string][]dataset.Set, selected []string, dir string) error {
	fmt.Fprintf(w, "datadir: %s\n\n", dir)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tARTIFACT\tACTIVE\tDATADIR\tEMBED\tREFRESHABLE")
	for _, set := range flattenSets(bySource, selected) {
		origin, err := set.Resolve(dir)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", set.Source, set.Processed.Name, err)
		}
		datadirState := "-"
		if n, ok := fileSize(filepath.Join(dir, set.Processed.Name)); ok {
			datadirState = humanBytes(n)
		}
		embedState := "no"
		if set.Embed != nil {
			embedState = "yes"
		}
		refreshable := "no"
		if set.Transform != nil {
			refreshable = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			set.Source, set.Processed.Name, origin, datadirState, embedState, refreshable)
	}
	return tw.Flush()
}

// printReport summarises a refresh Report on w.
func printReport(w io.Writer, report dataset.Report) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, r := range report {
		switch {
		case r.Err != nil:
			fmt.Fprintf(tw, "%s\t%s\tFAILED\t%v\n", r.Source, r.Processed, r.Err)
		case r.Skipped:
			fmt.Fprintf(tw, "%s\t%s\tskipped\t%s\n", r.Source, r.Processed, r.Reason)
		default:
			fmt.Fprintf(tw, "%s\t%s\tok\t%s\n", r.Source, r.Processed, humanBytes(r.Bytes))
		}
	}
	_ = tw.Flush()
}

// copyToEmbed implements --go-embed-update: it copies each successfully
// rebuilt processed artifact from the datadir into the in-repo embed
// directory sources/<source>/data/<name>, so the operator can re-commit the
// refreshed embedded data. It requires being run inside the module checkout.
func copyToEmbed(report dataset.Report, dir string) error {
	root, err := moduleRoot()
	if err != nil {
		return fmt.Errorf("--go-embed-update: %w", err)
	}
	for _, r := range report {
		if r.Err != nil {
			continue
		}
		if !r.Embedded {
			// Download-only dataset: it has no embed dir to update. Copying
			// it into sources/<name>/data would fabricate one the source
			// never reads.
			continue
		}
		src := filepath.Join(dir, r.Processed)
		if _, ok := fileSize(src); !ok {
			// Read-only set (no Transform), or nothing produced — nothing to
			// copy back into the repo.
			continue
		}
		dst := filepath.Join(root, "sources", r.Source, "data", r.Processed)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("--go-embed-update: copy %s: %w", r.Processed, err)
		}
		fmt.Fprintf(os.Stdout, "embed-update: %s -> %s\n", r.Processed, relTo(root, dst))
	}
	return nil
}

// moduleRoot returns the directory of the enclosing go.mod via `go env
// GOMOD`. It errors when not run inside a module checkout.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("not inside a Go module checkout")
	}
	return filepath.Dir(gomod), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is a datadir artifact we just wrote
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // in-repo data dir
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // in-repo data file
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}

func fileSize(p string) (int64, bool) {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return 0, false
	}
	return fi.Size(), true
}

// humanBytes renders a byte count as a compact human-readable string.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

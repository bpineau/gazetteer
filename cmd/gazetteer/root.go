package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// streams is the CLI's output plumbing: a sub-command writes its data to
// out and its usage banners, warnings and logs to err. Bundling the pair
// keeps the dispatch signatures short, and passing it explicitly (rather
// than reaching for os.Stdout deep inside a printer) is what lets a test
// drive a whole sub-command against a bytes.Buffer.
type streams struct {
	out io.Writer
	err io.Writer
}

// osStreams is the process's own pair, used by main.
func osStreams() streams { return streams{out: os.Stdout, err: os.Stderr} }

// usage prints the top-level help text.
func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: gazetteer <command> [flags] [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Typed real-estate-investment data for a French address, across every")
	fmt.Fprintln(w, "dimension (price, rents, demand, solvency, taxes, safety, transport, …).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  query      Collect every source's typed data for an address (the core); summary or --json.")
	fmt.Fprintln(w, "  sources    list | doc <name> | catalog [--json] | dimensions  — discover sources + their Result data.")
	fmt.Fprintln(w, "  appraise   Optional: query + consolidated price / rent / hazard + zone score.")
	fmt.Fprintln(w, "  compare    Optional: rank several addresses by zone score.")
	fmt.Fprintln(w, "  normalize  Resolve a free-text address into a canonical Listing via BAN.")
	fmt.Fprintln(w, "  refresh    [sources|all]       — download/rebuild datasets into the datadir; --list, --go-embed-update.")
	fmt.Fprintln(w, "  version    Print the gazetteer build version.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `gazetteer <command> -h` for sub-command flags.")
}

// errUsage is a sentinel signalling "print usage and exit non-zero
// without an extra error banner". Returned by sub-commands when the
// user mistypes flags / args.
var errUsage = errors.New("usage")

// run dispatches the first arg to its sub-command. Returns errUsage
// when the user typed garbage so main can map that to a non-zero exit
// without printing a redundant "gazetteer: usage" banner.
func run(ctx context.Context, args []string, w streams) error {
	if len(args) == 0 {
		usage(w.err)
		return errUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		return runVersion(ctx, rest, w)
	case "normalize":
		return runNormalize(ctx, rest, w)
	case "query":
		return runQuery(ctx, rest, w)
	case "appraise":
		return runAppraise(ctx, rest, w)
	case "compare":
		return runCompare(ctx, rest, w)
	case "sources":
		return runSources(ctx, rest, w)
	case "refresh":
		return runRefresh(ctx, rest, w)
	case "help", "-h", "--help":
		usage(w.out)
		return nil
	default:
		fmt.Fprintf(w.err, "gazetteer: unknown command %q\n\n", cmd)
		usage(w.err)
		return errUsage
	}
}

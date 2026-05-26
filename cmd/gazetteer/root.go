package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// usage prints the top-level help text.
func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: gazetteer <command> [flags] [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  query      Query configured sources for an address; print per-source summary or JSON.")
	fmt.Fprintln(w, "  appraise   Run query + synthesise consolidated price / rent / hazard view.")
	fmt.Fprintln(w, "  normalize  Resolve a free-text address into a canonical Listing via BAN.")
	fmt.Fprintln(w, "  sources    list | doc <name>  — inspect the registered Source catalogue.")
	fmt.Fprintln(w, "  refresh    <source>            — refresh embedded data (stub in v1).")
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
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		return runVersion(ctx, rest)
	case "normalize":
		return runNormalize(ctx, rest)
	case "query":
		return runQuery(ctx, rest)
	case "appraise":
		return runAppraise(ctx, rest)
	case "sources":
		return runSources(ctx, rest)
	case "refresh":
		return runRefresh(ctx, rest)
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "gazetteer: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return errUsage
	}
}

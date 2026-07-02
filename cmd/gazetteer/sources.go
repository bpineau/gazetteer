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

// runSources dispatches `gazetteer sources list` and `sources doc
// <name>`. The two share no flags; each gets its own FlagSet.
func runSources(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printSourcesUsage(os.Stderr)
		return errUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runSourcesList(ctx, rest)
	case "doc":
		return runSourcesDoc(ctx, rest)
	case "catalog":
		return runSourcesCatalog(rest)
	case "dimensions":
		return runSourcesDimensions(rest)
	case "-h", "--help", "help":
		printSourcesUsage(os.Stdout)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown sources sub-command %q\n\n", sub)
		printSourcesUsage(os.Stderr)
		return errUsage
	}
}

func printSourcesUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gazetteer sources list           List every Source registered with the lib (name + version).")
	fmt.Fprintln(w, "  gazetteer sources doc <name>     Print a JSON schema example of <name>'s typed Result.")
	fmt.Fprintln(w, "  gazetteer sources catalog [--json]  Full capability map: inputs, coverage, returns, feeds.")
	fmt.Fprintln(w, "  gazetteer sources dimensions     Sources grouped by investor-evaluation dimension.")
}

// runSourcesList prints one line per registered source. The name comes
// from gazetteer.RegisteredNames() (the lib-level registry, populated
// by each source package's init); the version comes from the CLI
// factory because the registry only knows the typed Result factory,
// not the Source.
func runSourcesList(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("sources list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer sources list")
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	// Build the catalog once so we can pair the registered names with
	// their Source.Version() — RegisteredNames alone doesn't yield it.
	deps := &runtimeDeps{}
	// Best-effort runtime deps so HTTP-backed factories don't panic.
	// `list` does not call Query, so a half-initialised deps suffices
	// — we still build a deps with the BAN client when possible.
	if d, err := newRuntimeDeps(); err == nil {
		deps = d
	}

	cat := sourceCatalog()
	byName := make(map[string]sourceFactory, len(cat))
	for _, f := range cat {
		byName[f.Name] = f
	}

	registered := gazetteer.RegisteredNames()
	// Always print the registered name (it's the canonical view); when
	// the CLI knows how to instantiate it, also print the Source's
	// reported Version. Otherwise mark it as "<no factory>".
	for _, name := range registered {
		f, ok := byName[name]
		if !ok {
			fmt.Printf("%-14s  (no CLI factory)\n", name)
			continue
		}
		src, err := f.Build(deps)
		if err != nil {
			fmt.Printf("%-14s  build error: %v\n", name, err)
			continue
		}
		dflt := ""
		if !f.Default {
			dflt = "  (opt-in via --source)"
		}
		fmt.Printf("%-14s  v%d%s\n", name, src.Version(), dflt)
	}
	return nil
}

// runSourcesDoc instantiates the typed Result via the lib's registry
// factory and prints it as indented JSON. Useful as a quick "what
// shape does dvf return?" reference without having to grep the
// source.
func runSourcesDoc(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("sources doc", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer sources doc <name>")
		fmt.Fprintln(fs.Output())
		fmt.Fprintf(fs.Output(), "Registered sources: %s\n", strings.Join(gazetteer.RegisteredNames(), ", "))
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	name := fs.Arg(0)

	factory := gazetteer.Lookup(name)
	if factory == nil {
		names := gazetteer.RegisteredNames()
		sort.Strings(names)
		return fmt.Errorf("unknown source %q (registered: %v)", name, names)
	}
	val := factory()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(val)
}

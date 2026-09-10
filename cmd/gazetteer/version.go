package main

import (
	"context"
	"flag"
	"fmt"
	"runtime/debug"
)

// version is overridden at build time via -ldflags "-X main.version=..."
// when packaging a release. The fallback "dev" suffices for local
// invocations.
var version = "dev"

// runVersion implements `gazetteer version`. Prints the build version
// plus the embedded VCS info from runtime/debug.BuildInfo when
// available (commit, dirty flag, build time) — useful when investigating
// a stale binary on a remote host.
func runVersion(_ context.Context, args []string, w streams) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(w.err)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer version")
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	fmt.Fprintf(w.out, "gazetteer %s\n", version)
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Fprintf(w.out, "  go      %s\n", info.GoVersion)
		fmt.Fprintf(w.out, "  module  %s\n", info.Main.Path)
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				fmt.Fprintf(w.out, "  %-14s %s\n", s.Key, s.Value)
			}
		}
	}
	return nil
}

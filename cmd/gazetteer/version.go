package main

import (
	"context"
	"flag"
	"fmt"
	"os"
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
func runVersion(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer version")
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}

	fmt.Printf("gazetteer %s\n", version)
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("  go      %s\n", info.GoVersion)
		fmt.Printf("  module  %s\n", info.Main.Path)
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				fmt.Printf("  %-14s %s\n", s.Key, s.Value)
			}
		}
	}
	return nil
}

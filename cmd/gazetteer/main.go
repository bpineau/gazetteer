// Command gazetteer is a CLI front-end for the gazetteer library.
//
// Sub-commands:
//
//	gazetteer query     [--source dvf,osm,...] [--json] [--verbose] [--dump] <addr>
//	gazetteer appraise  [--source dvf,osm,...] [--json] [--verbose] [--dump] <addr>
//	gazetteer normalize [--json] <addr>
//	gazetteer sources   list
//	gazetteer sources   doc <name>
//	gazetteer refresh   <source>                  (stub for v1)
//	gazetteer version
//
// Install:
//
//	go install github.com/bpineau/gazetteer/cmd/gazetteer@latest
//
// See doc/CLI.md for end-user documentation of every sub-command.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		// errUsage carries no useful detail beyond the help text the
		// command already printed; suppress the redundant banner.
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, "gazetteer:", err)
		}
		os.Exit(1)
	}
}

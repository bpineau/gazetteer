// Command gazetteer explores the library's data from the terminal: it
// lists and documents the Sources (sources list / catalog / dimensions /
// doc), runs them against real addresses (query, appraise, compare,
// normalize), and maintains the offline datasets (refresh). Run
// `gazetteer help` or see docs/cli.md for the full reference.
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

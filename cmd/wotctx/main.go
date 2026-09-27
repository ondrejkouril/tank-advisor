// Command wotctx caches World of Tanks account data and answers narrow questions
// about it, so a Claude skill can reason over real numbers instead of pasted ones.
//
// See docs/spec.md for the contract and docs/plan.md for the build order.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
)

// version is set by the linker; see the Makefile.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	env := cli.Bootstrap(os.Stdout, os.Stderr, version)

	switch err := cli.Run(ctx, env, os.Args[1:]); {
	case err == nil:
	case errors.Is(err, cli.ErrUsage):
		// Usage has already been written to stderr.
		os.Exit(2)
	default:
		// Errors can quote request URLs, so they are redacted on the way out.
		fmt.Fprintf(os.Stderr, "wotctx: %s\n", env.Redactor().RedactError(err))
		os.Exit(1)
	}
}

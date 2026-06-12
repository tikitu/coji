// Command coji is a CLI for reading, creating, and editing Confluence pages
// from markdown via the Confluence Cloud REST API v2.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	// Cancel in-flight work (e.g. the login wait) on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "coji:", err)
		os.Exit(1)
	}
}

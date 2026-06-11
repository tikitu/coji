package main

import (
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "coji",
		Short:         "Operate on Confluence pages from the command line",
		Long:          "coji reads, creates, and edits Confluence Cloud pages, converting to and from markdown.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newAuthCmd())
	root.AddCommand(newPageCmd())
	root.AddCommand(newSpaceCmd())

	return root
}

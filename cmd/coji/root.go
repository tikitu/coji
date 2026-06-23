package main

import (
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// policyPath is the path to the policy file, set by the --policy persistent
// flag and read by newService.
var policyPath string

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "coji",
		Short:         "Operate on Confluence pages from the command line",
		Long:          "coji reads, creates, and edits Confluence Cloud pages, converting to and from markdown.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&policyPath, "policy", "",
		"path to a policy file (default: <config dir>/policy.json, or $COJI_POLICY)")

	root.AddCommand(newAuthCmd())
	root.AddCommand(newPageCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newSpaceCmd())
	root.AddCommand(newBrowseCmd())
	root.AddCommand(newPolicyCmd())

	return root
}

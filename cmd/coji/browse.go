package main

import (
	"github.com/mgilbir/coji/internal/tui"
	"github.com/spf13/cobra"
)

func newBrowseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "browse [space-key-or-page-id]",
		Short: "Interactively browse the content tree",
		Long: `Open an interactive browser of the Confluence content tree.

With no argument, it lists your spaces; pass a space key to start there, or a
page ID to root the tree at that page. Navigate with the arrow keys, expand
with enter, view a page with "v", and press "y" to select an ID (printed on
exit, handy for "page create --parent").`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			start := ""
			if len(args) == 1 {
				start = args[0]
			}
			return tui.Run(cmd.Context(), svc, start)
		},
	}
}

package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSpaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "space",
		Short: "Browse spaces",
	}
	cmd.AddCommand(newSpaceListCmd())
	return cmd
}

func newSpaceListCmd() *cobra.Command {
	var key string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List spaces",
		Long:  "List spaces you can access. The KEY is what you pass to `page create --space`, and HOMEPAGE is a page ID you can use as a tree root (`coji page children <id> -r`).",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			spaces, err := svc.ListSpaces(cmd.Context(), key)
			if err != nil {
				return err
			}
			if len(spaces) == 0 {
				fmt.Println("No spaces found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "KEY\tID\tHOMEPAGE\tNAME")
			for _, s := range spaces {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Key, s.ID, s.HomepageID, s.Name)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", "filter to a single space key")
	return cmd
}

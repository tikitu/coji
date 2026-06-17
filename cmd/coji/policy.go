package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect the access policy",
	}
	cmd.AddCommand(newPolicyShowCmd())
	return cmd
}

func newPolicyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the resolved access policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := policyFilePath()
			if err != nil {
				return err
			}
			pol, err := loadPolicy()
			if err != nil {
				return err
			}
			fmt.Printf("policy file: %s\n", path)
			if pol == nil {
				fmt.Println("no policy file found — all operations are allowed.")
				return nil
			}
			fmt.Printf("default:          %s\n", pol.Default)
			fmt.Printf("personal-default: %s\n", pol.PersonalDefault)
			if len(pol.Spaces) == 0 {
				return nil
			}
			fmt.Println("per space:")
			keys := make([]string, 0, len(pol.Spaces))
			for k := range pol.Spaces {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, k := range keys {
				fmt.Fprintf(w, "  %s\t%s\n", k, pol.Spaces[k])
			}
			return w.Flush()
		},
	}
}

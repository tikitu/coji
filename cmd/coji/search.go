package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mgilbir/coji/internal/core"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var space string
	var limit int
	var rawCQL, asJSON bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search pages with CQL",
		Long: `Search Confluence pages and print each hit with an excerpt for context.

By default the query is matched against page text (CQL ` + "`type=page AND text ~ \"<query>\"`" + `);
use --cql to pass a raw CQL expression instead. --json emits a machine-readable
array (id, space, title, type, updated, url, excerpt) for feeding other tooling.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newService(cmd)
			if err != nil {
				return err
			}
			hits, err := svc.Search(cmd.Context(), args[0], core.SearchOptions{
				SpaceKey: space,
				Limit:    limit,
				RawCQL:   rawCQL,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return printSearchJSON(hits)
			}
			printSearchTable(hits)
			return nil
		},
	}
	cmd.Flags().StringVarP(&space, "space", "s", "", "restrict to a space key (e.g. ENG)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 25, "maximum number of results")
	cmd.Flags().BoolVar(&rawCQL, "cql", false, "treat the query as a raw CQL expression")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array instead of a table")
	return cmd
}

// searchJSON is the machine-readable shape of a search hit (lower-case keys),
// for tooling that triages hits and feeds page ids to a fetcher/archiver.
type searchJSON struct {
	ID      string `json:"id"`
	Space   string `json:"space"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Updated string `json:"updated"`
	URL     string `json:"url"`
	Excerpt string `json:"excerpt"`
}

func printSearchJSON(hits []core.SearchHit) error {
	out := make([]searchJSON, 0, len(hits))
	for _, h := range hits {
		out = append(out, searchJSON{
			ID:      h.ID,
			Space:   h.SpaceKey,
			Title:   h.Title,
			Type:    h.Type,
			Updated: h.Updated,
			URL:     h.WebURL,
			Excerpt: h.Excerpt,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printSearchTable(hits []core.SearchHit) {
	if len(hits) == 0 {
		fmt.Println("No results.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSPACE\tUPDATED\tTITLE")
	for _, h := range hits {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.ID, h.SpaceKey, shortDate(h.Updated), h.Title)
		if h.Excerpt != "" {
			fmt.Fprintf(w, "\t\t\t  %s\n", h.Excerpt)
		}
	}
	w.Flush()
}

// shortDate trims an ISO-8601 timestamp to its date for compact display.
func shortDate(ts string) string {
	if i := strings.IndexByte(ts, 'T'); i > 0 {
		return ts[:i]
	}
	return ts
}

package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newCollectionXrefCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xref <command>",
		Short: "Cross-reference a collection against other collections",
		Long: "Cross-referencing finds entities in other collections that are similar to the people, companies and\n" +
			"other things in a collection. Start it with `oa collection xref run`, then read the matches with\n" +
			"`oa collection xref list`. You only see matches from collections you can read.",
	}
	cmd.AddCommand(
		newCollectionXrefRunCmd(app),
		newCollectionXrefListCmd(app),
		newCollectionXrefExportCmd(app),
	)
	return cmd
}

func newCollectionXrefRunCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "run <collection>",
		Short: "Start cross-referencing a collection",
		Long: "Starts cross-referencing on the server and returns right away. The new results replace the earlier\n" +
			"ones. This needs write access to the collection. Follow it with `oa status watch <collection>`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			coll, err := c.ResolveCollection(ctx, args[0])
			if err != nil {
				return err
			}
			if err := c.StartXref(ctx, str(coll, "id")); err != nil {
				return err
			}
			fmt.Fprintf(app.IO.Err, "Started cross-referencing of %s\n", xrefCollectionName(coll))
			return nil
		},
	}
}

func newCollectionXrefListCmd(app *App) *cobra.Command {
	var (
		jf       jsonFlags
		matches  []string
		minScore float64
		schema   string
		query    string
		limit    int
	)
	cmd := &cobra.Command{
		Use:     "list <collection>",
		Aliases: []string{"ls"},
		Short:   "List the cross-referencing matches of a collection",
		Long: "Lists matches between entities of the collection and entities of other collections, best score\n" +
			"first. The server leaves out matches with a score of 0.5 or less.",
		Example: "  $ oa collection xref list leak_a --match leak_b --min-score 0.8\n" +
			"  $ oa collection xref list leak_a --json score,entity,match",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			coll, err := c.ResolveCollection(ctx, args[0])
			if err != nil {
				return err
			}
			o := api.XrefOptions{MinScore: minScore, Schema: schema, Query: query, Limit: min(max(limit, 1), 100)}
			for _, ref := range matches {
				m, err := c.ResolveCollection(ctx, ref)
				if err != nil {
					return err
				}
				o.MatchCollectionIDs = append(o.MatchCollectionIDs, str(m, "id"))
			}
			rs, err := c.Xref(ctx, str(coll, "id"), o)
			if err != nil {
				return err
			}
			var items []api.Object
			for len(items) < limit {
				item, ok := rs.Next(ctx)
				if !ok {
					break
				}
				items = append(items, item)
			}
			if err := rs.Err(); err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, items)
			}
			if len(items) == 0 {
				fmt.Fprintln(app.IO.Err, "No matches found")
				return nil
			}
			t := output.NewTable(app.IO.Out, "SCORE", "SCHEMA", "ENTITY", "MATCH", "MATCH COLLECTION", "JUDGEMENT")
			for _, it := range items {
				entity, _ := it["entity"].(map[string]any)
				match, _ := it["match"].(map[string]any)
				matchColl, _ := match["collection"].(map[string]any)
				t.Row(xrefScore(it["score"]), str(match, "schema"), str(entity, "caption"), str(match, "caption"),
					str(matchColl, "label"), str(it, "judgement"))
			}
			if err := t.Flush(); err != nil {
				return err
			}
			if rs.Total() > len(items) {
				fmt.Fprintf(app.IO.Err, "\nShowing %d of %d matches (use --limit)\n", len(items), rs.Total())
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&matches, "match", "m", nil, "Only matches from this `collection` (repeatable)")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "Only matches with at least this `score` (0 to 1)")
	cmd.Flags().StringVar(&schema, "schema", "", "Only matches of this `schema`")
	cmd.Flags().StringVarP(&query, "query", "Q", "", "Search the names of matching entities for `text`")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of matches to list")
	jf.add(cmd)
	return cmd
}

func newCollectionXrefExportCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "export <collection>",
		Short: "Export the cross-referencing matches as an Excel file",
		Long: "Asks the server to build an Excel file with all matches of the collection. The file is built in the\n" +
			"background and then listed under your exports (`oa api exports`), from where you can download it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			coll, err := c.ResolveCollection(ctx, args[0])
			if err != nil {
				return err
			}
			if err := c.ExportXref(ctx, str(coll, "id")); err != nil {
				return err
			}
			fmt.Fprintf(app.IO.Err, "Started the export %q\nIt is listed under /api/2/exports when it is ready (`oa api exports`)\n",
				str(coll, "label")+" - Cross-reference results")
			return nil
		},
	}
}

// xrefCollectionName returns the label of a collection, or its ID.
func xrefCollectionName(coll api.Object) string {
	if l := str(coll, "label"); l != "" {
		return l
	}
	return str(coll, "id")
}

// xrefScore formats a match score with two decimals.
func xrefScore(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', 2, 64)
	}
	return ""
}

package cmd

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newCollectionCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "collection <command>",
		Aliases: []string{"col"},
		Short:   "Manage collections",
		Long:    "Collections are referenced by numeric ID or by foreign ID.",
	}
	cmd.AddCommand(
		newCollectionListCmd(app),
		newCollectionViewCmd(app),
		newCollectionCreateCmd(app),
		newCollectionEditCmd(app),
		newCollectionDeleteCmd(app),
		newCollectionFlushCmd(app),
		newCollectionReindexCmd(app),
		newCollectionReingestCmd(app),
	)
	return cmd
}

func newCollectionListCmd(app *App) *cobra.Command {
	var (
		jf       jsonFlags
		query    string
		category string
		limit    int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List collections",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			params := url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}}
			var filters []api.Filter
			if category != "" {
				filters = append(filters, api.Filter{Key: "category", Value: category})
			}
			rs, err := c.ListCollections(ctx, query, filters, params)
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
				fmt.Fprintln(app.IO.Err, "No collections found")
				return nil
			}
			t := output.NewTable(app.IO.Out, "ID", "FOREIGN ID", "LABEL", "CATEGORY", "ENTITIES")
			for _, it := range items {
				t.Row(str(it, "id"), str(it, "foreign_id"), str(it, "label"), str(it, "category"), str(it, "count"))
			}
			if err := t.Flush(); err != nil {
				return err
			}
			if rs.Total() > len(items) {
				fmt.Fprintf(app.IO.Err, "\nShowing %d of %d collections (use --limit)\n", len(items), rs.Total())
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&query, "query", "Q", "", "Search collections by `text`")
	cmd.Flags().StringVar(&category, "category", "", "Only collections of this `category`")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of collections to list")
	jf.add(cmd)
	return cmd
}

func newCollectionViewCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "view <collection>",
		Short: "Show details of a collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			coll, err := c.ResolveCollection(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, coll)
			}
			out := app.IO.Out
			fmt.Fprintf(out, "%s\n", str(coll, "label"))
			for _, k := range []string{"id", "foreign_id", "category", "casefile", "count", "publisher", "countries", "languages", "created_at", "updated_at"} {
				if v := describe(coll[k]); v != "" {
					fmt.Fprintf(out, "  %-11s %s\n", k+":", v)
				}
			}
			if s := str(coll, "summary"); s != "" {
				fmt.Fprintf(out, "\n%s\n", s)
			}
			if links, ok := coll["links"].(map[string]any); ok {
				if ui := str(links, "ui"); ui != "" {
					fmt.Fprintf(out, "\n%s\n", ui)
				}
			}
			return nil
		},
	}
	jf.add(cmd)
	return cmd
}

// describe renders scalars and string lists for text output.
func describe(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case []any:
		var parts []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
			}
		}
		sort.Strings(parts)
		return strings.Join(parts, ", ")
	}
	return ""
}

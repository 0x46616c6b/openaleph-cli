package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newSearchCmd(app *App) *cobra.Command {
	var (
		jf         jsonFlags
		schema     string
		schemata   string
		collection string
		limit      int
		publisher  bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search entities",
		Long: "Search entities across all collections you can read. Without --schema or --schemata\n" +
			"only entities of the Thing schema (and its descendants) are searched.",
		Example: "  $ oa search \"acme corp\"\n" +
			"  $ oa search \"acme\" --schema Company --collection my_leak\n" +
			"  $ oa search \"acme\" --jq '.[].id'",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			var filters []api.Filter
			if collection != "" {
				coll, err := c.ResolveCollection(ctx, collection)
				if err != nil {
					return err
				}
				filters = append(filters, api.Filter{Key: "collection_id", Value: str(coll, "id")})
			}
			rs, err := c.Search(ctx, api.SearchOptions{
				Query:     strings.Join(args, " "),
				Schema:    schema,
				Schemata:  schemata,
				Filters:   filters,
				Publisher: publisher,
				Params:    url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}},
			})
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
				fmt.Fprintln(app.IO.Err, "No results")
				return nil
			}
			t := output.NewTable(app.IO.Out, "ID", "SCHEMA", "NAME", "COLLECTION")
			for _, it := range items {
				coll, _ := it["collection"].(map[string]any)
				t.Row(str(it, "id"), str(it, "schema"), caption(it), str(coll, "label"))
			}
			if err := t.Flush(); err != nil {
				return err
			}
			if rs.Total() > len(items) {
				fmt.Fprintf(app.IO.Err, "\nShowing %d of %d results (use --limit)\n", len(items), rs.Total())
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&schema, "schema", "s", "", "Only entities of exactly this `schema`")
	cmd.Flags().StringVar(&schemata, "schemata", "", "Only entities of this `schema` or its descendants")
	cmd.Flags().StringVarP(&collection, "collection", "c", "", "Only search in this `collection` (ID or foreign ID)")
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of results")
	cmd.Flags().BoolVarP(&publisher, "publisher", "p", false, "Add publisher info from the collection to JSON output")
	jf.add(cmd)
	return cmd
}

// caption returns a display name for an entity.
func caption(e api.Object) string {
	if s := str(e, "caption"); s != "" {
		return s
	}
	if props, ok := e["properties"].(map[string]any); ok {
		for _, k := range []string{"name", "title", "fileName"} {
			if vals, ok := props[k].([]any); ok && len(vals) > 0 {
				if s, ok := vals[0].(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

// relationCmd describes one of the paginated relation listings of an entity.
type relationCmd struct {
	use, short, long, example string
	// query adds a --query flag for free-text narrowing.
	query bool
	fetch func(c *api.Client, ctx context.Context, id string, o api.RelationOptions) (*api.ResultSet, error)
	table func(app *App, items []api.Object) error
}

func newEntitySimilarCmd(app *App) *cobra.Command {
	return newEntityRelationCmd(app, relationCmd{
		use:   "similar <id>",
		short: "List entities that are similar to an entity",
		long: "List entities that may be the same as the given entity, best match first. The server compares\n" +
			"names and other properties, and shows the judgement if the pair is part of a profile.",
		example: "  $ oa entity similar 0a1b2c --schema Person\n" +
			"  $ oa entity similar 0a1b2c --jq '.[] | select(.score > 0.7) | .entity.id'",
		fetch: (*api.Client).SimilarEntities,
		table: func(app *App, items []api.Object) error {
			t := output.NewTable(app.IO.Out, "ID", "SCHEMA", "NAME", "COLLECTION", "SCORE", "JUDGEMENT")
			for _, it := range items {
				e, _ := it["entity"].(map[string]any)
				score := ""
				if s, ok := it["score"].(float64); ok {
					score = strconv.FormatFloat(s, 'f', 2, 64)
				}
				t.Row(str(e, "id"), str(e, "schema"), api.Caption(e), collectionLabel(e), score, str(it, "judgement"))
			}
			return t.Flush()
		},
	})
}

func newEntityMentionsCmd(app *App) *cobra.Command {
	return newEntityRelationCmd(app, relationCmd{
		use:   "mentions <id>",
		short: "List documents that mention an entity",
		long: "List documents whose text contains the name of the given entity (a person, company or other named\n" +
			"entity) or one of its name variants.",
		example: "  $ oa entity mentions 0a1b2c\n" +
			"  $ oa entity mentions 0a1b2c --schema Email --query invoice",
		query: true,
		fetch: (*api.Client).EntityMentions,
		table: entityRelationTable,
	})
}

func newEntityNearbyCmd(app *App) *cobra.Command {
	return newEntityRelationCmd(app, relationCmd{
		use:   "nearby <id>",
		short: "List entities located near an address",
		long: "List entities with a location close to the given entity, nearest first. This only works for entities\n" +
			"with latitude and longitude, usually geocoded addresses. For other entities the list is empty.",
		example: "  $ oa entity nearby 0a1b2c --limit 10",
		fetch:   (*api.Client).NearbyEntities,
		table:   entityRelationTable,
	})
}

func newEntityRelationCmd(app *App, rc relationCmd) *cobra.Command {
	var (
		jf         jsonFlags
		schema     string
		schemata   string
		collection string
		q          string
		limit      int
	)
	cmd := &cobra.Command{
		Use:     rc.use,
		Short:   rc.short,
		Long:    rc.long,
		Example: rc.example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			filters := []api.Filter{{Key: "schema", Value: schema}, {Key: "schemata", Value: schemata}}
			if collection != "" {
				coll, err := c.ResolveCollection(ctx, collection)
				if err != nil {
					return err
				}
				filters = append(filters, api.Filter{Key: "collection_id", Value: str(coll, "id")})
			}
			rs, err := rc.fetch(c, ctx, args[0], api.RelationOptions{
				Query:   q,
				Filters: filters,
				Params:  url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}},
			})
			if err != nil {
				return err
			}
			items, err := takeItems(ctx, rs, limit)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, items)
			}
			if len(items) == 0 {
				fmt.Fprintln(app.IO.Err, "No results")
				return nil
			}
			if err := rc.table(app, items); err != nil {
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
	cmd.Flags().StringVarP(&collection, "collection", "c", "", "Only entities in this `collection` (ID or foreign ID)")
	if rc.query {
		cmd.Flags().StringVar(&q, "query", "", "Only results that also match this search `text`")
	}
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of results")
	jf.add(cmd)
	return cmd
}

// takeItems reads up to limit items from a result set.
func takeItems(ctx context.Context, rs *api.ResultSet, limit int) ([]api.Object, error) {
	var items []api.Object
	for len(items) < limit {
		item, ok := rs.Next(ctx)
		if !ok {
			break
		}
		items = append(items, item)
	}
	return items, rs.Err()
}

func entityRelationTable(app *App, items []api.Object) error {
	t := output.NewTable(app.IO.Out, "ID", "SCHEMA", "NAME", "COLLECTION")
	for _, e := range items {
		t.Row(str(e, "id"), str(e, "schema"), api.Caption(e), collectionLabel(e))
	}
	return t.Flush()
}

func collectionLabel(e api.Object) string {
	coll, _ := e["collection"].(map[string]any)
	return str(coll, "label")
}

func newEntityExpandCmd(app *App) *cobra.Command {
	var (
		jf         jsonFlags
		properties []string
		limit      int
	)
	cmd := &cobra.Command{
		Use:   "expand <id>",
		Short: "List entities connected to an entity",
		Long: "List the entities that are connected to the given entity, grouped by the property that connects\n" +
			"them. For a person this shows, for example, the directorships, ownerships and family relations\n" +
			"that point to it, together with the entities at their other end.",
		Example: "  $ oa entity expand 0a1b2c\n" +
			"  $ oa entity expand 0a1b2c --property directorshipDirector --limit 100\n" +
			"  $ oa entity expand 0a1b2c --jq '.[].entities[].id'",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			groups, err := c.ExpandEntity(cmd.Context(), args[0], properties, limit)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, groups)
			}
			if len(groups) == 0 {
				fmt.Fprintln(app.IO.Err, "No connected entities")
				return nil
			}
			t := output.NewTable(app.IO.Out, "PROPERTY", "ID", "SCHEMA", "NAME")
			truncated := false
			for _, g := range groups {
				for _, e := range g.Entities {
					t.Row(g.Property, str(e, "id"), str(e, "schema"), api.Caption(e))
				}
				if g.Count > len(g.Entities) {
					truncated = true
				}
			}
			if err := t.Flush(); err != nil {
				return err
			}
			if truncated {
				fmt.Fprintln(app.IO.Err, "\nSome properties have more connections than shown (use --limit or --json all)")
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&properties, "property", nil, "Only follow this `property` (repeatable)")
	cmd.Flags().IntVarP(&limit, "limit", "L", 0, "Maximum number of entities per property (server default 20, at most 200)")
	jf.add(cmd)
	return cmd
}

func newEntityTagsCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "tags <id>",
		Short: "List values that an entity shares with other entities",
		Long: "List the values of an entity, such as names, email addresses, phone numbers or IBANs, that other\n" +
			"entities have too, with the number of those entities. Search for a value to find them.",
		Example: "  $ oa entity tags 0a1b2c\n" +
			"  $ oa entity tags 0a1b2c --jq '.[] | select(.field == \"emails\") | .value'",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			tags, err := c.EntityTags(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, tags)
			}
			if len(tags) == 0 {
				fmt.Fprintln(app.IO.Err, "No shared values")
				return nil
			}
			t := output.NewTable(app.IO.Out, "FIELD", "VALUE", "OTHER ENTITIES")
			for _, tag := range tags {
				t.Row(tag.Field, tag.Value, strconv.Itoa(tag.Count))
			}
			return t.Flush()
		},
	}
	jf.add(cmd)
	return cmd
}

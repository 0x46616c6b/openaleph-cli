package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newEntitySetCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "entityset <command>",
		Aliases: []string{"es"},
		Short:   "Work with entity sets (lists, diagrams, timelines)",
	}
	cmd.AddCommand(newEntitySetListCmd(app), newEntitySetItemsCmd(app), newEntitySetCreateCmd(app), newEntitySetDeleteCmd(app))
	return cmd
}

func newEntitySetListCmd(app *App) *cobra.Command {
	var (
		jf         jsonFlags
		collection string
		types      []string
		prefix     string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List entity sets",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			collectionID := ""
			if collection != "" {
				coll, err := c.ResolveCollection(ctx, collection)
				if err != nil {
					return err
				}
				collectionID = str(coll, "id")
			}
			rs, err := c.EntitySets(ctx, collectionID, types, prefix)
			if err != nil {
				return err
			}
			items, err := rs.All(ctx)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, items)
			}
			t := output.NewTable(app.IO.Out, "ID", "TYPE", "LABEL", "COLLECTION")
			for _, it := range items {
				coll, _ := it["collection"].(map[string]any)
				t.Row(str(it, "id"), str(it, "type"), str(it, "label"), str(coll, "label"))
			}
			return t.Flush()
		},
	}
	cmd.Flags().StringVarP(&collection, "collection", "c", "", "Only entity sets of this `collection`")
	cmd.Flags().StringSliceVarP(&types, "type", "t", nil, "Only entity sets of this `type` (repeatable)")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Only entity sets whose label starts with `text`")
	jf.add(cmd)
	return cmd
}

func newEntitySetItemsCmd(app *App) *cobra.Command {
	var (
		jf        jsonFlags
		publisher bool
	)
	cmd := &cobra.Command{
		Use:   "items <entityset-id>",
		Short: "List the items of an entity set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			rs, err := c.EntitySetItems(ctx, args[0], publisher)
			if err != nil {
				return err
			}
			items, err := rs.All(ctx)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, items)
			}
			t := output.NewTable(app.IO.Out, "ID", "SCHEMA", "NAME")
			for _, it := range items {
				e, _ := it["entity"].(map[string]any)
				t.Row(str(e, "id"), str(e, "schema"), caption(e))
			}
			return t.Flush()
		},
	}
	cmd.Flags().BoolVarP(&publisher, "publisher", "p", false, "Add publisher info from the collection")
	jf.add(cmd)
	return cmd
}

func newEntitySetCreateCmd(app *App) *cobra.Command {
	var typ, summary string
	cmd := &cobra.Command{
		Use:   "create <collection> <label>",
		Short: "Create an entity set in a collection",
		Args:  cobra.ExactArgs(2),
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
			es, err := c.CreateEntitySet(ctx, str(coll, "id"), typ, args[1], summary)
			if err != nil {
				return err
			}
			fmt.Fprintln(app.IO.Out, str(es, "id"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&typ, "type", "t", "list", "Entity set `type` (list, diagram, timeline)")
	cmd.Flags().StringVar(&summary, "summary", "", "Entity set summary")
	return cmd
}

func newEntitySetDeleteCmd(app *App) *cobra.Command {
	var yes, sync bool
	cmd := &cobra.Command{
		Use:   "delete <entityset-id>",
		Short: "Delete an entity set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			if err := app.confirm(yes, "delete entity set "+args[0], args[0]); err != nil {
				return err
			}
			return c.DeleteEntitySet(cmd.Context(), args[0], sync)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&sync, "sync", false, "Wait for the deletion to complete")
	return cmd
}

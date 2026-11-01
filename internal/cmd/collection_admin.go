package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// collectionFlags are the editable collection properties.
type collectionFlags struct {
	foreignID string
	category  string
	summary   string
	languages []string
	casefile  bool
}

func (f *collectionFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.foreignID, "foreign-id", "", "Foreign ID of the collection (default: the label)")
	cmd.Flags().StringVar(&f.category, "category", "", "Collection `category` (for example leak, news, other)")
	cmd.Flags().StringVar(&f.summary, "summary", "", "Collection summary")
	cmd.Flags().StringSliceVarP(&f.languages, "language", "l", nil, "Language hint as 2-letter ISO 639 `code` (repeatable)")
	cmd.Flags().BoolVar(&f.casefile, "casefile", false, "Treat the collection as a case file")
}

func newCollectionCreateCmd(app *App) *cobra.Command {
	var f collectionFlags
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "create <label>",
		Short: "Create a collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			foreignID := f.foreignID
			if foreignID == "" {
				foreignID = args[0]
			}
			coll, err := c.LoadCollectionByForeignID(cmd.Context(), foreignID, api.CollectionConfig{
				Label: args[0], Casefile: f.casefile, Category: f.category,
				Languages: f.languages, Summary: f.summary,
			})
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, coll)
			}
			fmt.Fprintf(app.IO.Out, "%s\n", str(coll, "id"))
			return nil
		},
	}
	f.add(cmd)
	jf.add(cmd)
	return cmd
}

// editableKeys are the collection properties sent back on update.
var editableKeys = []string{
	"foreign_id", "label", "summary", "category", "languages", "casefile",
	"countries", "publisher", "publisher_url", "info_url", "data_url",
}

func newCollectionEditCmd(app *App) *cobra.Command {
	var f collectionFlags
	var label string
	var sync bool
	cmd := &cobra.Command{
		Use:   "edit <collection>",
		Short: "Edit collection properties",
		Args:  cobra.ExactArgs(1),
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
			data := api.Object{}
			for _, k := range editableKeys {
				if v, ok := coll[k]; ok && v != nil {
					data[k] = v
				}
			}
			changed := false
			set := func(flag, key string, val any) {
				if cmd.Flags().Changed(flag) {
					data[key] = val
					changed = true
				}
			}
			set("label", "label", label)
			set("foreign-id", "foreign_id", f.foreignID)
			set("category", "category", f.category)
			set("summary", "summary", f.summary)
			set("language", "languages", f.languages)
			set("casefile", "casefile", f.casefile)
			if !changed {
				return errors.New("nothing to change: pass at least one property flag")
			}
			_, err = c.UpdateCollection(ctx, str(coll, "id"), data, sync)
			return err
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "New label")
	cmd.Flags().BoolVar(&sync, "sync", false, "Wait until the change is indexed")
	f.add(cmd)
	return cmd
}

func newCollectionDeleteCmd(app *App) *cobra.Command {
	var yes, sync bool
	cmd := &cobra.Command{
		Use:   "delete <collection>",
		Short: "Delete a collection and all its contents",
		Args:  cobra.ExactArgs(1),
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
			name := firstNonEmpty(str(coll, "foreign_id"), str(coll, "id"))
			if err := app.confirm(yes, "delete collection "+name+" and everything in it", name); err != nil {
				return err
			}
			return c.DeleteCollection(ctx, str(coll, "id"), sync)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&sync, "sync", false, "Wait for the deletion to complete")
	return cmd
}

func newCollectionFlushCmd(app *App) *cobra.Command {
	var yes, sync bool
	cmd := &cobra.Command{
		Use:   "flush <collection>",
		Short: "Delete all contents of a collection but keep the collection",
		Args:  cobra.ExactArgs(1),
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
			name := firstNonEmpty(str(coll, "foreign_id"), str(coll, "id"))
			if err := app.confirm(yes, "delete all contents of collection "+name, name); err != nil {
				return err
			}
			return c.FlushCollection(ctx, str(coll, "id"), sync)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&sync, "sync", false, "Wait for the deletion to complete")
	return cmd
}

func newCollectionReindexCmd(app *App) *cobra.Command {
	var flush, sync bool
	cmd := &cobra.Command{
		Use:   "reindex <collection>",
		Short: "Re-index all entities in a collection",
		Args:  cobra.ExactArgs(1),
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
			return c.ReindexCollection(ctx, str(coll, "id"), flush, sync)
		},
	}
	cmd.Flags().BoolVar(&flush, "flush", false, "Flush entities before indexing")
	cmd.Flags().BoolVar(&sync, "sync", false, "Wait for indexing to complete")
	return cmd
}

func newCollectionReingestCmd(app *App) *cobra.Command {
	var index bool
	cmd := &cobra.Command{
		Use:   "reingest <collection>",
		Short: "Re-ingest all documents in a collection",
		Args:  cobra.ExactArgs(1),
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
			return c.ReingestCollection(ctx, str(coll, "id"), index)
		},
	}
	cmd.Flags().BoolVar(&index, "index", false, "Index documents as they are being processed")
	return cmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCancelCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel <collection>",
		Short: "Cancel the processing of a collection",
		Long: "Cancel the pending processing tasks of a collection, given by ID or foreign ID.\n" +
			"The server cancels in the background: pending tasks are dropped, running tasks finish.\n" +
			"Documents whose tasks were dropped stay unprocessed until the collection is reingested.\n" +
			"Needs write access to the collection.",
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
			name := firstNonEmpty(str(coll, "foreign_id"), str(coll, "id"))
			if err := app.confirm(yes, "cancel the processing of collection "+name, name); err != nil {
				return err
			}
			if err := c.CancelCollectionStatus(ctx, str(coll, "id")); err != nil {
				return err
			}
			fmt.Fprintf(app.IO.Err, "Requested cancellation for collection %s, running tasks finish first\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

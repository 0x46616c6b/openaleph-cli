package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newStatusCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "status [<collection>]",
		Short: "Show collections that are currently being processed",
		Long: "Without an argument, list all collections with pending or running tasks.\n" +
			"With a collection ID or foreign ID, show the processing status of that collection.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if len(args) == 1 {
				coll, err := c.ResolveCollection(ctx, args[0])
				if err != nil {
					return err
				}
				st, err := c.CollectionStatus(ctx, str(coll, "id"))
				if err != nil {
					return err
				}
				return jf.write(app, st) // no table form, the payload is task detail
			}
			st, err := c.Status(ctx)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, st)
			}
			results, _ := st["results"].([]any)
			if len(results) == 0 {
				fmt.Fprintln(app.IO.Err, "No collections are being processed")
				return nil
			}
			t := output.NewTable(app.IO.Out, "ID", "COLLECTION", "PENDING", "RUNNING", "FINISHED")
			for _, r := range results {
				o, _ := r.(map[string]any)
				coll, _ := o["collection"].(map[string]any)
				id := str(coll, "id")
				if id == "" {
					id = str(o, "collection_id")
				}
				label := str(coll, "label")
				if label == "" {
					label = str(o, "name")
				}
				t.Row(id, label, str(o, "pending"), str(o, "running"), str(o, "finished"))
			}
			return t.Flush()
		},
	}
	jf.add(cmd)
	return cmd
}

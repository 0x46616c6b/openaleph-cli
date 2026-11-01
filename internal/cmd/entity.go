package cmd

import (
	"github.com/spf13/cobra"
)

func newEntityCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity <command>",
		Short: "Work with entities",
	}
	cmd.AddCommand(newEntityViewCmd(app))
	return cmd
}

func newEntityViewCmd(app *App) *cobra.Command {
	var (
		jf        jsonFlags
		publisher bool
	)
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "Show an entity as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			e, err := c.GetEntity(cmd.Context(), args[0], publisher)
			if err != nil {
				return err
			}
			// Entity properties are free-form, so JSON is the only sensible text form.
			return jf.write(app, e)
		},
	}
	cmd.Flags().BoolVarP(&publisher, "publisher", "p", false, "Add publisher info from the collection")
	jf.add(cmd)
	return cmd
}

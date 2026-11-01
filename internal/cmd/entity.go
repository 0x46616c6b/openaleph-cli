package cmd

import (
	"github.com/spf13/cobra"
)

func newEntityCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity <command>",
		Short: "Work with entities",
	}
	cmd.AddCommand(
		newEntityViewCmd(app),
		newEntityDeleteCmd(app),
		newEntityStreamCmd(app),
		newEntityWriteCmd(app),
		newEntityWriteManyCmd(app),
	)
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

func newEntityDeleteCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			if err := app.confirm(yes, "delete entity "+args[0], args[0]); err != nil {
				return err
			}
			return c.DeleteEntity(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

func newEntitySetViewCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "view <entityset-id>",
		Short: "Show details of an entity set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			es, err := c.GetEntitySet(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, es)
			}
			out := app.IO.Out
			fmt.Fprintf(out, "%s\n", str(es, "label"))
			coll, _ := es["collection"].(map[string]any)
			fields := [][2]string{
				{"id", str(es, "id")},
				{"type", str(es, "type")},
				{"collection", str(coll, "label")},
				{"writeable", str(es, "writeable")},
				{"created_at", str(es, "created_at")},
				{"updated_at", str(es, "updated_at")},
			}
			for _, f := range fields {
				if f[1] != "" {
					fmt.Fprintf(out, "  %-11s %s\n", f[0]+":", f[1])
				}
			}
			if s := str(es, "summary"); s != "" {
				fmt.Fprintf(out, "\n%s\n", s)
			}
			return nil
		},
	}
	jf.add(cmd)
	return cmd
}

func newEntitySetEditCmd(app *App) *cobra.Command {
	var label, summary string
	cmd := &cobra.Command{
		Use:   "edit <entityset-id>",
		Short: "Change the label or summary of an entity set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data := api.Object{}
			if cmd.Flags().Changed("label") {
				if label == "" {
					return errors.New("the label must not be empty")
				}
				data["label"] = label
			}
			if cmd.Flags().Changed("summary") {
				data["summary"] = summary
			}
			if len(data) == 0 {
				return errors.New("nothing to change: pass --label or --summary")
			}
			c, err := app.client()
			if err != nil {
				return err
			}
			_, err = c.UpdateEntitySet(cmd.Context(), args[0], data)
			return err
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "New label")
	cmd.Flags().StringVar(&summary, "summary", "", "New summary (empty to clear)")
	return cmd
}

var addJudgements = []string{api.JudgementPositive, api.JudgementNegative, api.JudgementUnsure}

func newEntitySetAddCmd(app *App) *cobra.Command {
	var judgement string
	cmd := &cobra.Command{
		Use:   "add <entityset-id> <entity-id>...",
		Short: "Add existing entities to an entity set",
		Long: "Add existing entities to an entity set, or change their judgement if they are already in it.\n" +
			"The entities may come from any collection you can read; they are not copied.\n" +
			"Only entities judged \"positive\" count as members of the set; `oa entityset items` also lists\n" +
			"\"negative\" and \"unsure\" ones, which are mostly used for profiles.",
		Example: "  $ oa entityset add 3a0d91ec e1 e2\n" +
			"  $ oa entityset add 3a0d91ec e3 --judgement negative",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			valid := false
			for _, j := range addJudgements {
				valid = valid || j == judgement
			}
			if !valid {
				return fmt.Errorf("invalid judgement %q: use positive, negative or unsure", judgement)
			}
			return setEntitySetItems(cmd, app, args[0], args[1:], judgement)
		},
	}
	cmd.Flags().StringVar(&judgement, "judgement", api.JudgementPositive, "Judgement: positive, negative or unsure")
	return cmd
}

func newEntitySetRemoveCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <entityset-id> <entity-id>...",
		Aliases: []string{"rm"},
		Short:   "Remove entities from an entity set",
		Long: "Remove entities from an entity set. The entities themselves are not deleted, so `oa entityset add`\n" +
			"undoes this. Entities that are not in the set are ignored.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setEntitySetItems(cmd, app, args[0], args[1:], api.JudgementNone)
		},
	}
	return cmd
}

func setEntitySetItems(cmd *cobra.Command, app *App, setID string, entityIDs []string, judgement string) error {
	c, err := app.client()
	if err != nil {
		return err
	}
	for _, id := range entityIDs {
		if _, err := c.SetEntitySetItem(cmd.Context(), setID, id, judgement); err != nil {
			return fmt.Errorf("entity %s: %w", id, err)
		}
	}
	return nil
}

func newEntitySetWriteCmd(app *App) *cobra.Command {
	var infile string
	cmd := &cobra.Command{
		Use:   "write <entityset-id>",
		Short: "Create or update entities and add them to an entity set",
		Long: "Read entities as JSON lines from standard input, write each one and add it to the entity set.\n" +
			"New entities are created in the collection of the entity set. An existing entity that you cannot\n" +
			"edit is only added to the set. Prints the ID of each entity.\n\n" +
			"This sends one request per entity. To load many entities into the set's collection, use\n" +
			"`oa entity write-many <collection> --entityset <entityset-id>`.",
		Example: "  $ oa entity stream my_leak --schema Person | oa entityset write 3a0d91ec",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			in, closeIn, err := app.openIn(infile)
			if err != nil {
				return err
			}
			defer func() { _ = closeIn() }()
			n := 0
			for e, err := range jsonLines(in) {
				if err != nil {
					return err
				}
				out, err := c.WriteEntitySetEntity(cmd.Context(), args[0], e)
				if err != nil {
					return fmt.Errorf("entity %d: %w", n+1, err)
				}
				n++
				fmt.Fprintln(app.IO.Out, str(out, "id"))
			}
			if n == 0 {
				return errors.New("no entity found on input")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&infile, "infile", "i", "-", "Read from this `file` instead of standard input")
	return cmd
}

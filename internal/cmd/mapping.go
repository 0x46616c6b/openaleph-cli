package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newMappingCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mapping <command>",
		Short: "Manage entity mappings stored in OpenAleph",
		Long: "Mappings turn a table that is already in a collection into entities on the server.\n" +
			"Create one from a mapping file, then run it with `oa mapping trigger`.\n" +
			"For mapping CSV files on your own machine see `oa entity map`.",
	}
	cmd.AddCommand(
		newMappingListCmd(app), newMappingViewCmd(app), newMappingCreateCmd(app), newMappingUpdateCmd(app),
		newMappingDeleteCmd(app), newMappingTriggerCmd(app), newMappingFlushCmd(app),
	)
	return cmd
}

// loadMappingQuery reads a mapping file and returns the query to store on the server.
// It accepts a file in the format of `ftm map` with exactly one query, or a bare
// query. The server keeps only the entities and supplies the source table, so
// csv_url is dropped.
func loadMappingQuery(path string) (api.Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	query := doc
	if queries, ok := doc["queries"]; ok {
		list, _ := queries.([]any)
		if len(list) != 1 {
			return nil, fmt.Errorf("%s: a mapping on the server has exactly one query, found %d", path, len(list))
		}
		if query, ok = list[0].(map[string]any); !ok {
			return nil, fmt.Errorf("%s: query must be a mapping", path)
		}
	}
	entities, ok := query["entities"].(map[string]any)
	if !ok || len(entities) == 0 {
		return nil, fmt.Errorf("%s: no entities defined", path)
	}
	for _, k := range []string{"filters", "filters_not"} {
		if query[k] != nil {
			return nil, fmt.Errorf("%s: the server stores only the entities of a mapping, remove %q", path, k)
		}
	}
	out := api.Object(entities)
	// Round-trip through JSON to make sure the body can be encoded.
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var clean api.Object
	return clean, json.Unmarshal(b, &clean)
}

// mappingCollection resolves a collection reference and returns its numeric ID.
func (a *App) mappingCollection(cmd *cobra.Command, c *api.Client, ref string) (string, error) {
	coll, err := c.ResolveCollection(cmd.Context(), ref)
	if err != nil {
		return "", err
	}
	return str(coll, "id"), nil
}

func newMappingListCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:     "list <collection>",
		Aliases: []string{"ls"},
		Short:   "List the mappings of a collection",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			rs, err := c.Mappings(ctx, cid)
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
			t := output.NewTable(app.IO.Out, "ID", "TABLE", "STATUS", "UPDATED")
			for _, it := range items {
				t.Row(str(it, "id"), str(it, "table_id"), str(it, "last_run_status"), str(it, "updated_at"))
			}
			return t.Flush()
		},
	}
	jf.add(cmd)
	return cmd
}

func newMappingViewCmd(app *App) *cobra.Command {
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "view <collection> <mapping-id>",
		Short: "Show a mapping and its query",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			m, err := c.GetMapping(cmd.Context(), cid, args[1])
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, m)
			}
			out := app.IO.Out
			fmt.Fprintf(out, "mapping %s\n", str(m, "id"))
			for _, k := range []string{"table_id", "entityset_id", "last_run_status", "last_run_err_msg", "created_at", "updated_at"} {
				if v := describe(m[k]); v != "" {
					fmt.Fprintf(out, "  %-17s %s\n", k+":", v)
				}
			}
			if q := m["query"]; q != nil {
				b, err := yaml.Marshal(q)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "\n%s", b)
			}
			return nil
		},
	}
	jf.add(cmd)
	return cmd
}

func newMappingCreateCmd(app *App) *cobra.Command {
	var table string
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "create <collection> <mapping.yml>",
		Short: "Create a mapping for a table in a collection",
		Long: "The mapping file is in the format of `ftm map`. Only the entities of its single query are stored on\n" +
			"the server and csv_url is ignored, because the source is the table entity given with --table.",
		Example: "  $ oa mapping create my_leak people.yml --table 1b3a9c\n" +
			"  $ oa mapping trigger my_leak 12",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := loadMappingQuery(args[1])
			if err != nil {
				return err
			}
			c, err := app.client()
			if err != nil {
				return err
			}
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			m, err := c.CreateMapping(cmd.Context(), cid, table, query)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, m)
			}
			fmt.Fprintln(app.IO.Out, str(m, "id"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&table, "table", "t", "", "ID of the table `entity` the mapping reads from")
	_ = cmd.MarkFlagRequired("table")
	jf.add(cmd)
	return cmd
}

func newMappingUpdateCmd(app *App) *cobra.Command {
	var table string
	var jf jsonFlags
	cmd := &cobra.Command{
		Use:   "update <collection> <mapping-id> [mapping.yml]",
		Short: "Change the query or table of a mapping",
		Long: "Give a mapping file to replace the query, --table to point the mapping at another table, or both.\n" +
			"Entities generated earlier are kept until you run `oa mapping flush`.",
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 3 && table == "" {
				return errors.New("nothing to update: pass a mapping file or --table")
			}
			var query api.Object
			if len(args) == 3 {
				q, err := loadMappingQuery(args[2])
				if err != nil {
					return err
				}
				query = q
			}
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			// The API replaces both fields, so keep what the caller did not change.
			if query == nil || table == "" {
				cur, err := c.GetMapping(ctx, cid, args[1])
				if err != nil {
					return err
				}
				if table == "" {
					table = str(cur, "table_id")
				}
				if query == nil {
					query, _ = cur["query"].(map[string]any)
				}
			}
			m, err := c.UpdateMapping(ctx, cid, args[1], table, query)
			if err != nil {
				return err
			}
			if jf.enabled() {
				return jf.write(app, m)
			}
			fmt.Fprintln(app.IO.Out, str(m, "id"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&table, "table", "t", "", "ID of the table `entity` the mapping reads from")
	jf.add(cmd)
	return cmd
}

func newMappingDeleteCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <collection> <mapping-id>",
		Short: "Delete a mapping (generated entities are kept)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			if err := app.confirm(yes, "delete mapping "+args[1], args[1]); err != nil {
				return err
			}
			return c.DeleteMapping(cmd.Context(), cid, args[1])
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func newMappingTriggerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger <collection> <mapping-id>",
		Short: "Generate entities from the mapping's table",
		Long:  "Starts the mapping on the server and returns right away. Follow it with `oa status watch <collection>`.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			return c.TriggerMapping(cmd.Context(), cid, args[1])
		},
	}
	return cmd
}

func newMappingFlushCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "flush <collection> <mapping-id>",
		Short: "Delete the entities a mapping generated",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			cid, err := app.mappingCollection(cmd, c, args[0])
			if err != nil {
				return err
			}
			if err := app.confirm(yes, "delete all entities generated by mapping "+args[1], args[1]); err != nil {
				return err
			}
			return c.FlushMapping(cmd.Context(), cid, args[1])
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/fetch"
	"github.com/0x46616c6b/openaleph-cli/internal/output"
)

func newExportCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <command>",
		Short: "Create, list and download server-side exports",
		Long: "Exports are built by the server in the background. Start one, follow it with\n" +
			"`oa export list` and download it with `oa export download` once it is finished.\n" +
			"The server deletes exports after 30 days.",
	}
	cmd.AddCommand(
		newExportSearchCmd(app),
		newExportListCmd(app),
		newExportDownloadCmd(app),
	)
	return cmd
}

func newExportSearchCmd(app *App) *cobra.Command {
	var (
		schema     string
		schemata   string
		collection string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Export the results of a search as a zip archive",
		Long: "Start a server-side export of a search. The archive contains an Excel file with the\n" +
			"matching entities and the files of matching documents, up to the limit set by the server\n" +
			"(10,000 results by default). The filters work like those of `oa search`.",
		Example: "  $ oa export search \"acme\" --schema Company --collection my_leak\n" +
			"  $ oa export list",
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
			err = c.ExportSearch(ctx, api.SearchOptions{
				Query:    strings.Join(args, " "),
				Schema:   schema,
				Schemata: schemata,
				Filters:  filters,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(app.IO.Err, "Export started. Run `oa export list` to see when it is ready.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&schema, "schema", "s", "", "Only entities of exactly this `schema`")
	cmd.Flags().StringVar(&schemata, "schemata", "", "Only entities of this `schema` or its descendants")
	cmd.Flags().StringVarP(&collection, "collection", "c", "", "Only export from this `collection` (ID or foreign ID)")
	return cmd
}

func newExportListCmd(app *App) *cobra.Command {
	var (
		jf    jsonFlags
		limit int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List your exports",
		Long:    "List your exports, newest first. Deleted and expired exports are not shown.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			rs, err := c.ListExports(ctx, url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}})
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
				fmt.Fprintln(app.IO.Err, "No exports found")
				return nil
			}
			t := output.NewTable(app.IO.Out, "ID", "LABEL", "STATUS", "SIZE", "CREATED", "EXPIRES")
			for _, it := range items {
				t.Row(str(it, "id"), str(it, "label"), str(it, "status"), byteSize(it["file_size"]),
					shortTime(str(it, "created_at")), shortTime(str(it, "expires_at")))
			}
			if err := t.Flush(); err != nil {
				return err
			}
			if rs.Total() > len(items) {
				fmt.Fprintf(app.IO.Err, "\nShowing %d of %d exports (use --limit)\n", len(items), rs.Total())
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of exports to list")
	jf.add(cmd)
	return cmd
}

func newExportDownloadCmd(app *App) *cobra.Command {
	var (
		target    string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a finished export",
		Long: "Download a finished export. The file is saved under the name given by the server in the\n" +
			"current directory, or at --output (a file name or an existing directory).\n" +
			"An existing file is kept unless --overwrite is given.",
		Example: "  $ oa export download 42\n" +
			"  $ oa export download 42 -o results.zip",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			e, err := c.GetExport(ctx, args[0])
			if err != nil {
				return err
			}
			link := api.ExportDownloadURL(e)
			if link == "" {
				if str(e, "status") == "failed" {
					return fmt.Errorf("export %s failed", args[0])
				}
				return fmt.Errorf("export %s is not ready yet (status: %s)", args[0], str(e, "status"))
			}
			name := fetch.SafeName(str(e, "file_name"), "export-"+args[0]+".zip")
			path := name
			if target != "" {
				path = target
				if fi, err := os.Stat(target); err == nil && fi.IsDir() {
					path = filepath.Join(target, name)
				}
			}
			if err := fetch.File(ctx, c, link, path, overwrite); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return fmt.Errorf("%s already exists (use --overwrite to replace it)", path)
				}
				return err
			}
			fmt.Fprintf(app.IO.Err, "Saved %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&target, "output", "o", "", "Destination `file` or directory")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Replace the file if it already exists")
	return cmd
}

// byteSize formats a JSON number of bytes for humans, or returns "".
func byteSize(v any) string {
	n, ok := v.(float64)
	if !ok {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%.0f B", n)
	}
	exp := 0
	for n >= unit*unit && exp < 4 {
		n /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", n/unit, "KMGTP"[exp])
}

// shortTime shortens an ISO timestamp from the API to minutes.
func shortTime(s string) string {
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}

package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/mapping"
)

func newEntityMapCmd(app *App) *cobra.Command {
	var outfile string
	cmd := &cobra.Command{
		Use:   "map <mapping.yml>",
		Short: "Create entities from CSV files with a mapping",
		Long: "Read a mapping file in the format of `ftm map` and print the entities it creates as JSON lines.\n" +
			"Nothing is sent to OpenAleph; pipe the output to `oa entity write-many` to load it.\n\n" +
			"Only CSV sources (csv_url, a path, file:// or http(s)://) are supported. Relative paths are resolved\n" +
			"against the directory of the mapping file. Rows that produce the same entity ID are merged, and the\n" +
			"entities are held in memory until all files are read. Property values are validated by the server\n" +
			"when they are written, not here. The generated IDs are stable for the same dataset and keys, but are\n" +
			"not guaranteed to equal the IDs of `ftm map`.",
		Example: "  $ oa entity map people.yml > people.ftm.json\n" +
			"  $ oa entity map people.yml | oa entity write-many my_leak",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			m, err := mapping.Load(args[0])
			if err != nil {
				return err
			}
			out, closeOut, err := app.openOut(outfile)
			if err != nil {
				return err
			}
			defer func() {
				if cerr := closeOut(); err == nil {
					err = cerr
				}
			}()

			w := bufio.NewWriterSize(out, 1<<16)
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			err = m.Run(cmd.Context(), filepath.Dir(args[0]), func(e mapping.Entity) error {
				return enc.Encode(e)
			})
			if ferr := w.Flush(); err == nil {
				err = ferr
			}
			if errors.Is(err, syscall.EPIPE) {
				return nil // reader went away, like `| head`
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&outfile, "outfile", "o", "-", "Write to this `file` instead of standard output")
	return cmd
}

package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

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
			t := output.NewTable(app.IO.Out, "ID", "COLLECTION", "TODO", "DOING", "FAILED", "FINISHED", "REMAINING")
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
				t.Row(id, label, str(o, "todo"), str(o, "doing"), str(o, "failed"), str(o, "finished"), remaining(str(o, "remaining_time")))
			}
			return t.Flush()
		},
	}
	jf.add(cmd)
	return cmd
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// remaining renders an ISO 8601 duration such as PT5H12M42.8S as "5h12m".
// Unparsable values are returned unchanged, zero durations as "-".
func remaining(iso string) string {
	m := isoDuration.FindStringSubmatch(iso)
	if m == nil {
		return iso
	}
	num := func(s string) int {
		f, _ := strconv.ParseFloat(s, 64)
		return int(f)
	}
	days, hours, mins, secs := num(m[1]), num(m[2]), num(m[3]), num(m[4])
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if len(parts) == 0 {
		if secs > 0 || mins > 0 {
			return fmt.Sprintf("%dm%ds", mins, secs)
		}
		return "-"
	}
	return strings.Join(parts, "")
}

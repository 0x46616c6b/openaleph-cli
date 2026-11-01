package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
			return renderStatus(app.IO.Out, results)
		},
	}
	jf.add(cmd)
	cmd.AddCommand(newStatusWatchCmd(app))
	return cmd
}

// renderStatus prints one table row per collection in a /status result list.
func renderStatus(w io.Writer, results []any) error {
	t := output.NewTable(w, "ID", "COLLECTION", "TODO", "DOING", "FAILED", "FINISHED", "REMAINING")
	for _, r := range results {
		o, _ := r.(map[string]any)
		t.Row(statusID(o), statusLabel(o), str(o, "todo"), str(o, "doing"), str(o, "failed"), str(o, "finished"), remaining(str(o, "remaining_time")))
	}
	return t.Flush()
}

func statusID(o map[string]any) string {
	coll, _ := o["collection"].(map[string]any)
	if id := str(coll, "id"); id != "" {
		return id
	}
	return str(o, "collection_id")
}

func statusLabel(o map[string]any) string {
	coll, _ := o["collection"].(map[string]any)
	if label := str(coll, "label"); label != "" {
		return label
	}
	return str(o, "name")
}

func num(o map[string]any, key string) float64 {
	f, _ := o[key].(float64)
	return f
}

// listenForQuit puts an interactive stdin into raw mode and calls cancel when
// q, Esc or Ctrl-C is pressed. The returned function restores the terminal and
// is safe to call more than once. Without a terminal it does nothing.
func listenForQuit(app *App, cancel context.CancelFunc) func() {
	f, ok := app.IO.In.(*os.File)
	if !ok || !app.IO.InTTY {
		return func() {}
	}
	state, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return func() {}
	}
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			switch buf[0] {
			case 'q', 'Q', 27, 3:
				cancel()
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { _ = term.Restore(int(f.Fd()), state) }) }
}

func newStatusWatchCmd(app *App) *cobra.Command {
	var (
		interval   time.Duration
		exitStatus bool
	)
	cmd := &cobra.Command{
		Use:   "watch [<collection>]",
		Short: "Watch processing progress until all tasks have finished",
		Long: "Poll the processing status and refresh it until no tasks are pending or running.\n" +
			"With a collection ID or foreign ID, only that collection is watched.\n" +
			"On a terminal a live table is drawn in place and q stops watching;\n" +
			"otherwise every change is appended as plain text.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval <= 0 {
				return errors.New("--interval must be positive")
			}
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			quit := listenForQuit(app, cancel)
			defer quit()
			if app.IO.OutTTY {
				fmt.Fprint(app.IO.Out, "\x1b[?25l\x1b[2J") // hide cursor, clear screen
				defer fmt.Fprint(app.IO.Out, "\x1b[?25h")
			}
			var only string
			if len(args) == 1 {
				coll, err := c.ResolveCollection(ctx, args[0])
				if err != nil {
					return err
				}
				only = str(coll, "id")
			}
			failed := map[string]float64{} // last seen failure count, kept after a collection leaves the list
			var last string
			for {
				st, err := c.Status(ctx)
				if err != nil {
					if ctx.Err() != nil && cmd.Context().Err() == nil {
						quit()
						fmt.Fprintln(app.IO.Err, "Stopped watching")
						return nil
					}
					return err
				}
				var rows []any
				all, _ := st["results"].([]any)
				for _, r := range all {
					o, _ := r.(map[string]any)
					if only != "" && statusID(o) != only {
						continue
					}
					failed[statusID(o)] = num(o, "failed")
					if num(o, "todo")+num(o, "doing") > 0 {
						rows = append(rows, r)
					}
				}
				if len(rows) == 0 {
					break
				}
				if app.IO.OutTTY {
					frame := renderStatusTUI(rows, time.Now(), interval, app.Getenv("NO_COLOR") == "")
					fmt.Fprint(app.IO.Out, "\x1b[H"+frame+"\x1b[J")
				} else {
					var buf bytes.Buffer
					if err := renderStatus(&buf, rows); err != nil {
						return err
					}
					if frame := buf.String(); frame != last {
						if last != "" {
							fmt.Fprintln(app.IO.Out)
						}
						fmt.Fprint(app.IO.Out, frame)
						last = frame
					}
				}
				select {
				case <-ctx.Done():
					if cmd.Context().Err() == nil {
						quit()
						fmt.Fprintln(app.IO.Err, "Stopped watching")
						return nil
					}
					return ctx.Err()
				case <-time.After(interval):
				}
			}
			var total float64
			for _, n := range failed {
				total += n
			}
			if total > 0 {
				fmt.Fprintf(app.IO.Err, "All tasks finished, %.0f failed\n", total)
				if exitStatus {
					return errors.New("some tasks failed")
				}
				return nil
			}
			fmt.Fprintln(app.IO.Err, "All tasks finished")
			return nil
		},
	}
	cmd.Flags().DurationVarP(&interval, "interval", "i", 3*time.Second, "Refresh interval")
	cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit with a non-zero status if any task failed")
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

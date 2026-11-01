// Package cmd wires the oa command tree.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/config"
)

// version is set at build time via -ldflags.
var version = "dev"

// NewRootCmd builds the command tree for the given app.
func NewRootCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "oa <command>",
		Short:         "OpenAleph on the command line",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(app.IO.In)
	root.SetOut(app.IO.Out)
	root.SetErr(app.IO.Err)

	pf := root.PersistentFlags()
	pf.StringVar(&app.host, "host", "", "OpenAleph host URL (overrides OA_HOST, OPAL_HOST and the saved login)")
	pf.StringVar(&app.apiKey, "api-key", "", "OpenAleph API key (overrides OA_API_KEY, OPAL_API_KEY and the saved login)")
	pf.IntVarP(&app.retries, "retries", "r", 0, "Retries upon server failure (default 5, or OPENALEPH_MAX_TRIES)")

	root.AddCommand(
		newAuthCmd(app),
		newStatusCmd(app),
		newCollectionCmd(app),
		newSearchCmd(app),
		newEntityCmd(app),
		newAPICmd(app),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	store, err := config.DefaultStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	app := &App{IO: SystemIO(), Getenv: os.Getenv, Store: store}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := NewRootCmd(app).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(app.IO.Err, "Error:", err)
		return 1
	}
	return 0
}

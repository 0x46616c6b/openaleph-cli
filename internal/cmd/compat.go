package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// addCompatCommands registers the top-level commands of the Python
// `openaleph` client under their old names and flags, hidden from help.
// Collections are always addressed by foreign ID (-f) here. Unlike the Python
// client, destructive commands (delete, flush, delete-entity) ask for
// confirmation or need --yes. The global --host, --api-key and --retries flags
// work as before.
func addCompatCommands(root *cobra.Command, app *App) {
	root.AddCommand(
		compatCrawldir(app),
		compatFetchdir(app),
		compatReingest(app),
		compatReindex(app),
		compatDelete(app),
		compatFlush(app),
		compatWriteEntity(app),
		compatDeleteEntity(app),
		compatWriteEntities(app),
		compatStreamEntities(app),
		compatEntitysets(app),
		compatEntitysetitems(app),
		compatMakeList(app),
	)
}

func compat(use, short string, args cobra.PositionalArgs, run func(cmd *cobra.Command, args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:    use,
		Short:  short + " (compatible with the Python openaleph client)",
		Hidden: true,
		Args:   args,
		RunE:   run,
	}
}

func foreignFlag(cmd *cobra.Command, dst *string, required bool) {
	cmd.Flags().StringVarP(dst, "foreign-id", "f", "", "foreign_id of the collection")
	if required {
		_ = cmd.MarkFlagRequired("foreign-id")
	}
}

func compatCrawldir(app *App) *cobra.Command {
	o := crawlOpts{foreignOnly: true}
	cmd := compat("crawldir <path>", "Crawl a directory recursively and upload the documents in it", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			o.path = args[0]
			return app.crawlDir(cmd.Context(), o)
		})
	f := cmd.Flags()
	foreignFlag(cmd, &o.collection, true)
	f.BoolVar(&o.casefile, "casefile", false, "handle as case file")
	f.BoolVarP(&o.noindex, "noindex", "i", false, "do not index documents after ingest")
	f.StringSliceVarP(&o.languages, "language", "l", nil, "language hint: 2-letter language code (ISO 639)")
	f.IntVarP(&o.parallel, "parallel", "p", 1, "maximum number of parallel uploads")
	f.BoolVar(&o.resume, "resume", false, "Resume from an existing state file")
	f.StringVar(&o.stateFile, "state-file", "", "Path to state file (for resuming from custom locations)")
	return cmd
}

func compatFetchdir(app *App) *cobra.Command {
	o := fetchOpts{foreignOnly: true}
	cmd := compat("fetchdir", "Recursively download an entity or collection as a folder tree", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error { return app.fetchDir(cmd.Context(), o) })
	f := cmd.Flags()
	foreignFlag(cmd, &o.collection, false)
	f.StringVarP(&o.entityID, "entity-id", "e", "", "id of the root entity to download")
	f.StringVarP(&o.prefix, "prefix", "p", "", "destination path for the download")
	f.BoolVar(&o.overwrite, "overwrite", false, "overwrite existing files")
	return cmd
}

// withCollection resolves the -f foreign ID and runs fn with the client.
func (a *App) withCollection(cmd *cobra.Command, foreignID string, fn func(ctx context.Context, c *api.Client, id string) error) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	coll, err := a.collectionByRef(ctx, c, foreignID, true)
	if err != nil {
		return err
	}
	return fn(ctx, c, api.ID(coll))
}

func compatReingest(app *App) *cobra.Command {
	var fid string
	var index bool
	cmd := compat("reingest", "Trigger a re-ingest on all the documents in the collection", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			return app.withCollection(cmd, fid, func(ctx context.Context, c *api.Client, id string) error {
				return c.ReingestCollection(ctx, id, index)
			})
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().BoolVar(&index, "index", false, "index documents as they are being processed")
	return cmd
}

func compatReindex(app *App) *cobra.Command {
	var fid string
	var flush bool
	cmd := compat("reindex", "Trigger a re-index of all the entities in the collection", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			return app.withCollection(cmd, fid, func(ctx context.Context, c *api.Client, id string) error {
				return c.ReindexCollection(ctx, id, flush, false)
			})
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().BoolVar(&flush, "flush", false, "flush entities before indexing")
	return cmd
}

func compatDelete(app *App) *cobra.Command {
	var fid string
	var sync, yes bool
	cmd := compat("delete", "Delete a collection and all its contents", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			return app.withCollection(cmd, fid, func(ctx context.Context, c *api.Client, id string) error {
				if err := app.confirm(yes, "delete collection "+fid+" and everything in it", fid); err != nil {
					return err
				}
				return c.DeleteCollection(ctx, id, sync)
			})
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().BoolVar(&sync, "sync", false, "wait for delete to complete")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func compatFlush(app *App) *cobra.Command {
	var fid string
	var sync, yes bool
	cmd := compat("flush", "Delete all the contents of a collection", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			return app.withCollection(cmd, fid, func(ctx context.Context, c *api.Client, id string) error {
				if err := app.confirm(yes, "delete all contents of collection "+fid, fid); err != nil {
					return err
				}
				return c.FlushCollection(ctx, id, sync)
			})
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().BoolVar(&sync, "sync", false, "wait for delete to complete")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func compatWriteEntity(app *App) *cobra.Command {
	var fid, infile string
	cmd := compat("write-entity", "Read a single entity from standard input and index it", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			return app.writeEntity(cmd.Context(), fid, true, infile, "")
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().StringVarP(&infile, "infile", "i", "-", "input file")
	return cmd
}

func compatDeleteEntity(app *App) *cobra.Command {
	var yes bool
	cmd := compat("delete-entity <entity_id>", "Delete a single entity", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			if err := app.confirm(yes, "delete entity "+args[0], args[0]); err != nil {
				return err
			}
			return c.DeleteEntity(cmd.Context(), args[0])
		})
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func compatWriteEntities(app *App) *cobra.Command {
	o := writeOpts{foreignOnly: true}
	cmd := compat("write-entities", "Read entities from standard input and index them", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error { return app.writeEntities(cmd.Context(), o) })
	f := cmd.Flags()
	foreignFlag(cmd, &o.collection, true)
	f.StringVarP(&o.infile, "infile", "i", "-", "input file")
	f.StringVarP(&o.entitySet, "entityset", "e", "", "add entities to the given entity set")
	f.IntVarP(&o.chunk, "chunksize", "c", 1000, "chunk size when sending to API")
	f.BoolVar(&o.force, "force", false, "continue after server errors")
	f.BoolVar(&o.unsafe, "unsafe", false, "allow references to archive hashes")
	f.BoolVar(&o.cleaned, "cleaned", false, "disable server-side validation for all types")
	return cmd
}

func compatStreamEntities(app *App) *cobra.Command {
	o := streamOpts{foreignOnly: true}
	cmd := compat("stream-entities", "Load entities from the server and print them to stdout", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			if o.collection == "" {
				return errors.New("a collection is required: use -f")
			}
			return app.streamEntities(cmd.Context(), o)
		})
	f := cmd.Flags()
	foreignFlag(cmd, &o.collection, false)
	f.StringVarP(&o.outfile, "outfile", "o", "-", "output file")
	f.StringArrayVarP(&o.schemas, "schema", "s", nil, "schema (repeatable)")
	f.BoolVarP(&o.publisher, "publisher", "p", false, "Add publisher info from collection context")
	return cmd
}

// writeJSONLines prints one compact JSON object per line.
func (a *App) writeJSONLines(path string, items []api.Object) (err error) {
	out, closeOut, err := a.openOut(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := closeOut(); err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			if errors.Is(err, syscall.EPIPE) {
				return nil
			}
			return err
		}
	}
	return nil
}

func compatEntitysets(app *App) *cobra.Command {
	var fid, outfile string
	var types []string
	cmd := compat("entitysets", "Stream all entity sets", cobra.NoArgs,
		func(cmd *cobra.Command, _ []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			collectionID := ""
			if fid != "" {
				coll, err := app.collectionByRef(ctx, c, fid, true)
				if err != nil {
					return err
				}
				collectionID = api.ID(coll)
			}
			rs, err := c.EntitySets(ctx, collectionID, types, "")
			if err != nil {
				return err
			}
			items, err := rs.All(ctx)
			if err != nil {
				return err
			}
			return app.writeJSONLines(outfile, items)
		})
	foreignFlag(cmd, &fid, false)
	cmd.Flags().StringVarP(&outfile, "outfile", "o", "-", "output file")
	cmd.Flags().StringSliceVarP(&types, "type", "t", nil, "entity set type")
	return cmd
}

func compatEntitysetitems(app *App) *cobra.Command {
	var outfile string
	cmd := compat("entitysetitems <entityset_id>", "Stream all items of an entity set", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			rs, err := c.EntitySetItems(ctx, args[0], false)
			if err != nil {
				return err
			}
			items, err := rs.All(ctx)
			if err != nil {
				return err
			}
			return app.writeJSONLines(outfile, items)
		})
	cmd.Flags().StringVarP(&outfile, "outfile", "o", "-", "output file")
	return cmd
}

func compatMakeList(app *App) *cobra.Command {
	var fid, outfile, summary string
	cmd := compat("make-list <label>", "Create a list", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			return app.withCollection(cmd, fid, func(ctx context.Context, c *api.Client, id string) error {
				es, err := c.CreateEntitySet(ctx, id, "list", args[0], summary)
				if err != nil {
					return err
				}
				out, closeOut, err := app.openOut(outfile)
				if err != nil {
					return err
				}
				// Like the Python client, no trailing newline.
				_, werr := io.WriteString(out, api.ID(es))
				return errors.Join(werr, closeOut())
			})
		})
	foreignFlag(cmd, &fid, true)
	cmd.Flags().StringVarP(&outfile, "outfile", "o", "-", "output file")
	cmd.Flags().StringVarP(&summary, "summary", "s", "", "summary")
	return cmd
}

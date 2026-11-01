package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/crawl"
	"github.com/0x46616c6b/openaleph-cli/internal/fetch"
)

// logger prints progress in the format of the Python client:
// 2006-01-02 15:04:05 | INFO     |  message
func (a *App) logger() func(level, msg string) {
	return func(level, msg string) {
		fmt.Fprintf(a.IO.Err, "%s | %-8s |  %s\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)
	}
}

type crawlOpts struct {
	collection  string
	foreignOnly bool
	path        string
	languages   []string
	casefile    bool
	noindex     bool
	parallel    int
	resume      bool
	stateFile   string
}

func (a *App) crawlDir(ctx context.Context, o crawlOpts) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	coll, err := a.loadCollection(ctx, c, o.collection, o.foreignOnly, api.CollectionConfig{
		Languages: o.languages,
		Casefile:  o.casefile,
	})
	if err != nil {
		return err
	}
	res, err := crawl.Run(ctx, c, coll, crawl.Options{
		Root:      o.path,
		Parallel:  o.parallel,
		Index:     !o.noindex,
		Resume:    o.resume,
		StateFile: o.stateFile,
		Log:       a.logger(),
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errors.New("interrupted")
		}
		return err
	}
	if res.Failed > 0 {
		return fmt.Errorf("%d files failed to upload", res.Failed)
	}
	return nil
}

type fetchOpts struct {
	collection  string
	foreignOnly bool
	entityID    string
	prefix      string
	overwrite   bool
}

func (a *App) fetchDir(ctx context.Context, o fetchOpts) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	fo := fetch.Options{Prefix: o.prefix, Overwrite: o.overwrite, Log: a.logger()}
	switch {
	case o.entityID != "":
		_, err = fetch.Entity(ctx, c, o.entityID, fo)
	case o.collection != "":
		var coll api.Object
		if coll, err = a.collectionByRef(ctx, c, o.collection, o.foreignOnly); err != nil {
			return err
		}
		_, err = fetch.Collection(ctx, c, coll, fo)
	default:
		return errors.New("please specify either a collection or an entity ID")
	}
	return err
}

func newCollectionUploadCmd(app *App) *cobra.Command {
	var o crawlOpts
	cmd := &cobra.Command{
		Use:   "upload <collection> <path>",
		Short: "Upload a directory recursively into a collection",
		Long: "Crawl a directory and upload its files, keeping the folder structure.\n\n" +
			"If the collection is given as a foreign ID that does not exist yet, it is created.\n" +
			"Progress is kept in .openaleph_crawl_state.db (the same format as the Python client), so an\n" +
			"interrupted upload can be continued with --resume. A new upload refuses to start while a\n" +
			"state file exists: pass --resume, or remove the file to start over.\n" +
			"Paths matching a pattern in the .openalephignore file of the directory are skipped.",
		Example: "  $ oa collection upload my_leak ./documents --parallel 4\n" +
			"  $ oa collection upload my_leak ./documents --resume",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.collection, o.path = args[0], args[1]
			return app.crawlDir(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.IntVarP(&o.parallel, "parallel", "p", 1, "Maximum number of parallel uploads")
	f.BoolVar(&o.resume, "resume", false, "Resume from an existing state file")
	f.StringVar(&o.stateFile, "state-file", "", "Path to the state `file` (for resuming from custom locations)")
	f.BoolVarP(&o.noindex, "noindex", "i", false, "Do not index documents after ingest")
	f.StringSliceVarP(&o.languages, "language", "l", nil, "Language hint as 2-letter ISO 639 `code` (repeatable)")
	f.BoolVar(&o.casefile, "casefile", false, "Create the collection as a case file")
	return cmd
}

func newCollectionDownloadCmd(app *App) *cobra.Command {
	var o fetchOpts
	cmd := &cobra.Command{
		Use:   "download <collection>",
		Short: "Download all documents of a collection into a folder tree",
		Long: "Rebuild the folders and files of a collection on disk.\n\n" +
			"Existing files are kept unless --overwrite is given.",
		Example: "  $ oa collection download my_leak --output ./out",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.collection = args[0]
			return app.fetchDir(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVarP(&o.prefix, "output", "o", "", "Destination `directory` (default: the current directory)")
	cmd.Flags().BoolVar(&o.overwrite, "overwrite", false, "Replace files that already exist")
	return cmd
}

func newEntityDownloadCmd(app *App) *cobra.Command {
	var o fetchOpts
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a document or folder entity and everything below it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.entityID = args[0]
			return app.fetchDir(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVarP(&o.prefix, "output", "o", "", "Destination `directory` (default: the current directory)")
	cmd.Flags().BoolVar(&o.overwrite, "overwrite", false, "Replace files that already exist")
	return cmd
}

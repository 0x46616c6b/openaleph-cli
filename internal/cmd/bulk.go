package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// collectionByRef finds a collection. With foreignOnly the reference is
// always a foreign ID, as in the Python client's -f flag.
func (a *App) collectionByRef(ctx context.Context, c *api.Client, ref string, foreignOnly bool) (api.Object, error) {
	if !foreignOnly {
		return c.ResolveCollection(ctx, ref)
	}
	coll, err := c.CollectionByForeignID(ctx, ref)
	if err != nil {
		return nil, err
	}
	if coll == nil {
		return nil, fmt.Errorf("collection %q does not exist", ref)
	}
	return coll, nil
}

// loadCollection is like collectionByRef but creates missing collections
// when the reference is a foreign ID.
func (a *App) loadCollection(ctx context.Context, c *api.Client, ref string, foreignOnly bool, cfg api.CollectionConfig) (api.Object, error) {
	if _, err := strconv.ParseUint(ref, 10, 64); err == nil && !foreignOnly {
		return c.GetCollection(ctx, ref)
	}
	return c.LoadCollectionByForeignID(ctx, ref, cfg)
}

func (a *App) openOut(path string) (io.Writer, func() error, error) {
	if path == "" || path == "-" {
		return a.IO.Out, func() error { return nil }, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

func (a *App) openIn(path string) (io.Reader, func() error, error) {
	if path == "" || path == "-" {
		return a.IO.In, func() error { return nil }, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

type streamOpts struct {
	collection  string
	foreignOnly bool
	schemas     []string
	publisher   bool
	outfile     string
}

// streamEntities writes all entities of a collection as JSON lines.
func (a *App) streamEntities(ctx context.Context, o streamOpts) (err error) {
	c, err := a.client()
	if err != nil {
		return err
	}
	coll, err := a.collectionByRef(ctx, c, o.collection, o.foreignOnly)
	if err != nil {
		return err
	}
	out, closeOut, err := a.openOut(o.outfile)
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
	err = c.StreamEntities(ctx, api.StreamOptions{
		Collection: coll,
		Include:    []string{"id", "schema", "properties"},
		Schemas:    o.schemas,
		Publisher:  o.publisher,
	}, func(e api.Object) error { return enc.Encode(e) })
	if ferr := w.Flush(); err == nil {
		err = ferr
	}
	if errors.Is(err, syscall.EPIPE) {
		return nil // reader went away, like `| head`
	}
	return err
}

// jsonLines yields one decoded JSON object per non-empty input line.
func jsonLines(r io.Reader) iter.Seq2[api.Object, error] {
	return func(yield func(api.Object, error) bool) {
		br := bufio.NewReaderSize(r, 1<<20)
		for n := 1; ; n++ {
			line, err := br.ReadBytes('\n')
			if len(strings.TrimSpace(string(line))) > 0 {
				var e api.Object
				if jerr := json.Unmarshal(line, &e); jerr != nil {
					yield(nil, fmt.Errorf("line %d: %w", n, jerr))
					return
				}
				if !yield(e, nil) {
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(nil, err)
				}
				return
			}
		}
	}
}

type writeOpts struct {
	collection  string
	foreignOnly bool
	infile      string
	chunk       int
	entitySet   string
	force       bool
	unsafe      bool
	cleaned     bool
}

// writeEntities bulk-indexes JSON lines into a collection.
func (a *App) writeEntities(ctx context.Context, o writeOpts) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	coll, err := a.loadCollection(ctx, c, o.collection, o.foreignOnly, api.CollectionConfig{})
	if err != nil {
		return err
	}
	in, closeIn, err := a.openIn(o.infile)
	if err != nil {
		return err
	}
	defer func() { _ = closeIn() }()

	label := firstNonEmpty(str(coll, "foreign_id"), str(coll, "id"))
	progress := false
	err = c.WriteEntities(ctx, str(coll, "id"), jsonLines(in), api.BulkOptions{
		ChunkSize:   o.chunk,
		EntitySetID: o.entitySet,
		Unsafe:      o.unsafe,
		Cleaned:     o.cleaned,
		Force:       o.force,
		OnError: func(err error) {
			a.clearProgress(progress)
			progress = false
			fmt.Fprintln(a.IO.Err, "Error:", err)
		},
		OnChunk: func(total int) {
			if a.IO.ErrTTY {
				fmt.Fprintf(a.IO.Err, "\r\x1b[K[%s] Bulk load entities: %d...", label, total)
				progress = true
				return
			}
			fmt.Fprintf(a.IO.Err, "[%s] Bulk load entities: %d...\n", label, total)
		},
	})
	a.clearProgress(progress)
	return err
}

func (a *App) clearProgress(active bool) {
	if active {
		fmt.Fprintln(a.IO.Err)
	}
}

// writeEntity indexes the first JSON object of the input.
func (a *App) writeEntity(ctx context.Context, collection string, foreignOnly bool, infile, entityID string) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	coll, err := a.loadCollection(ctx, c, collection, foreignOnly, api.CollectionConfig{})
	if err != nil {
		return err
	}
	in, closeIn, err := a.openIn(infile)
	if err != nil {
		return err
	}
	defer func() { _ = closeIn() }()
	for e, err := range jsonLines(in) {
		if err != nil {
			return err
		}
		_, err = c.WriteEntity(ctx, str(coll, "id"), e, entityID)
		return err
	}
	return errors.New("no entity found on input")
}

func newEntityStreamCmd(app *App) *cobra.Command {
	var o streamOpts
	cmd := &cobra.Command{
		Use:   "stream <collection>",
		Short: "Print all entities of a collection as JSON lines",
		Example: "  $ oa entity stream my_leak --schema Person > people.ftm.json\n" +
			"  $ oa entity stream my_leak | oa entity write-many other_collection",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.collection = args[0]
			return app.streamEntities(cmd.Context(), o)
		},
	}
	cmd.Flags().StringArrayVarP(&o.schemas, "schema", "s", nil, "Only entities of this `schema` (repeatable)")
	cmd.Flags().BoolVarP(&o.publisher, "publisher", "p", false, "Add publisher info from the collection context")
	cmd.Flags().StringVarP(&o.outfile, "outfile", "o", "-", "Write to this `file` instead of standard output")
	return cmd
}

func newEntityWriteCmd(app *App) *cobra.Command {
	var infile, entityID string
	cmd := &cobra.Command{
		Use:   "write <collection>",
		Short: "Index a single entity read from standard input",
		Long:  "Read one JSON entity from standard input and index it. The collection is created if a foreign ID does not exist yet.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.writeEntity(cmd.Context(), args[0], false, infile, entityID)
		},
	}
	cmd.Flags().StringVarP(&infile, "infile", "i", "-", "Read from this `file` instead of standard input")
	cmd.Flags().StringVar(&entityID, "id", "", "Entity ID, overrides the id in the input")
	return cmd
}

func newEntityWriteManyCmd(app *App) *cobra.Command {
	var o writeOpts
	cmd := &cobra.Command{
		Use:   "write-many <collection>",
		Short: "Bulk-index entities read from standard input",
		Long: "Read entities as JSON lines from standard input and index them in chunks.\n" +
			"The collection is created if a foreign ID does not exist yet.",
		Example: "  $ oa entity write-many my_leak < entities.ftm.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.collection = args[0]
			return app.writeEntities(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.infile, "infile", "i", "-", "Read from this `file` instead of standard input")
	f.StringVarP(&o.entitySet, "entityset", "e", "", "Add the entities to this entity set `id`")
	f.IntVarP(&o.chunk, "chunk-size", "c", 1000, "Number of entities per request")
	f.BoolVar(&o.force, "force", false, "Continue after server errors")
	f.BoolVar(&o.unsafe, "unsafe", false, "Allow references to archive hashes")
	f.BoolVar(&o.cleaned, "cleaned", false, "Disable server-side validation for all types")
	return cmd
}

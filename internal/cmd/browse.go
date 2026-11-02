package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
	"github.com/0x46616c6b/openaleph-cli/internal/fetch"
	"github.com/0x46616c6b/openaleph-cli/internal/tui"
)

// browsePageSize is the number of rows fetched per request in the browser.
const browsePageSize = 50

func newBrowseCmd(app *App) *cobra.Command {
	var (
		collection string
		search     string
		status     bool
		schemas    []string
		debug      string
	)
	cmd := &cobra.Command{
		Use:     "browse",
		Aliases: []string{"tui"},
		Short:   "Browse collections and entities in an interactive terminal UI",
		Long: "Browse collections, search entities and look at their properties in a full-screen\n" +
			"terminal UI. Press ? inside the browser for all keys.\n\n" +
			"  enter, l     open the selected row\n" +
			"  esc, h       go back\n" +
			"  tab          switch between datasets and investigations, or between the\n" +
			"               entities and the documents (folders) of a collection\n" +
			"  d            download a document or folder\n" +
			"  /            search (enter runs it, esc cancels)\n" +
			"  j/k, g/G     move, jump to top or bottom\n" +
			"  y            copy the ID to the clipboard (OSC 52, works over SSH)\n" +
			"  o            open in the web browser\n" +
			"  J            show the raw JSON of an entity\n" +
			"  f, F         filter entities by schema, clear the filter\n" +
			"  p            show the processing status (refreshes every 3s)\n" +
			"  a            collection actions: n new, e edit, D delete, i re-index, I re-ingest\n" +
			"  ctrl+z       suspend to the shell (fg returns)\n" +
			"  q            quit",
		Example: "  $ oa browse\n" +
			"  $ oa browse --collection leak_a\n" +
			"  $ oa browse --search \"acme corp\"\n" +
			"  $ oa browse --collection leak_a --search \"acme\"\n" +
			"  $ oa browse --collection leak_a --schema Person,Address\n" +
			"  $ oa browse --status",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !app.IO.InTTY || !app.IO.OutTTY {
				return errors.New("oa browse needs an interactive terminal")
			}
			s, err := app.settings()
			if err != nil {
				return err
			}
			if debug != "" {
				// The log holds search terms and collection IDs, so only the
				// user may read it.
				f, err := os.OpenFile(debug, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				defer f.Close()
				fmt.Fprintf(f, "%s oa %s browse %s\n", time.Now().Format(time.RFC3339), version, s.Host)
				base := http.DefaultTransport
				if app.HTTP != nil && app.HTTP.Transport != nil {
					base = app.HTTP.Transport
				}
				app.HTTP = &http.Client{Transport: api.LoggingTransport(base, f)}
			}
			c, err := app.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			var coll api.Object
			if collection != "" {
				if coll, err = c.ResolveCollection(ctx, collection); err != nil {
					return err
				}
			}
			return tui.Run(ctx, tui.Options{
				Backend:     browseBackend{c},
				Host:        s.Host,
				Collection:  coll,
				Query:       search,
				Status:      status,
				Schemas:     schemas,
				Open:        openBrowser,
				DownloadDir: cwd,
				In:          app.IO.In,
				Out:         app.IO.Out,
			})
		},
	}
	cmd.Flags().StringVarP(&collection, "collection", "c", "", "Start in this `collection` (ID or foreign ID)")
	cmd.Flags().StringVarP(&search, "search", "s", "", "Start with an entity search for `text`")
	cmd.Flags().BoolVar(&status, "status", false, "Start in the processing status")
	cmd.Flags().StringVar(&debug, "debug", "", "Log all API requests and errors to `file`")
	cmd.Flags().StringSliceVar(&schemas, "schema", nil, "Only entities of this `schema` (repeatable, or comma-separated)")
	cmd.MarkFlagsMutuallyExclusive("collection", "status")
	cmd.MarkFlagsMutuallyExclusive("search", "status")
	cmd.MarkFlagsMutuallyExclusive("schema", "status")
	return cmd
}

// browseBackend loads single pages for the browser, which fetches more as the
// cursor moves down, and changes collections.
type browseBackend struct{ c *api.Client }

var (
	_ tui.CollectionAdmin = browseBackend{}
	_ tui.Downloader      = browseBackend{}
)

// pageParams asks for the page at offset, but not past the paging limit of
// the server.
func pageParams(offset int) url.Values {
	limit := max(min(browsePageSize, tui.MaxResults-offset), 0)
	return url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
}

func toPage(rs *api.ResultSet, err error) (tui.Page, error) {
	if err != nil {
		return tui.Page{}, err
	}
	p := rs.Page()
	return tui.Page{Items: p.Results, Total: p.Total, TotalAtLeast: p.TotalType == "gte", More: p.Next != ""}, nil
}

// Collections splits collections like the web UI: investigations are the
// collections of the category casefile, datasets all others.
func (b browseBackend) Collections(ctx context.Context, q string, investigations bool, offset int) (tui.Page, error) {
	params := pageParams(offset)
	var filters []api.Filter
	if investigations {
		filters = append(filters, api.Filter{Key: "category", Value: "casefile"})
	} else {
		params.Set("exclude:category", "casefile")
	}
	return toPage(b.c.ListCollections(ctx, q, filters, params))
}

// entitySearch builds the search for q. Folders work like in the web UI: the
// contents of a folder are the documents whose parent it is, a search in a
// folder covers all documents below it (ancestors), and the top level of a
// collection are the documents without a parent.
func entitySearch(q tui.EntityQuery) api.SearchOptions {
	o := api.SearchOptions{Query: q.Query, Schemas: q.Schemas, Params: url.Values{}}
	if q.CollectionID != "" {
		o.Filters = []api.Filter{{Key: "collection_id", Value: q.CollectionID}}
	}
	switch {
	case q.Parent != "":
		field := "properties.parent"
		if q.Query != "" {
			field = "properties.ancestors"
		}
		o.Filters = append(o.Filters, api.Filter{Key: field, Value: q.Parent})
		o.Schemata = "Document"
	case q.TopLevel:
		o.Params.Set("empty:properties.parent", "true")
		o.Schemata = "Document"
	}
	return o
}

func (b browseBackend) Entities(ctx context.Context, q tui.EntityQuery, offset int) (tui.Page, error) {
	o := entitySearch(q)
	for k, v := range pageParams(offset) {
		o.Params[k] = v
	}
	return toPage(b.c.Search(ctx, o))
}

// Download saves a document or a folder tree like `oa entity download`.
func (b browseBackend) Download(ctx context.Context, entityID, dir string, overwrite bool,
	progress func(tui.DownloadResult, string)) (tui.DownloadResult, error) {
	res, err := fetch.Entity(ctx, b.c, entityID, fetch.Options{
		Prefix:    dir,
		Overwrite: overwrite,
		Progress: func(r fetch.Result, name string) {
			progress(tui.DownloadResult{Fetched: r.Fetched, Skipped: r.Skipped}, name)
		},
	})
	return tui.DownloadResult{Fetched: res.Fetched, Skipped: res.Skipped}, err
}

// SchemaCounts counts over all schemas, relations like Ownership included,
// so they can be picked even though the unfiltered list shows only Things.
func (b browseBackend) SchemaCounts(ctx context.Context, q tui.EntityQuery) ([]api.FacetValue, error) {
	o := entitySearch(q)
	o.Schemas, o.AllSchemata = nil, true
	return b.c.SearchFacet(ctx, o, "schema", 200)
}

func (b browseBackend) Entity(ctx context.Context, id string) (api.Object, error) {
	return b.c.GetEntity(ctx, id, false)
}

func (b browseBackend) Status(ctx context.Context) ([]api.Object, error) {
	st, err := b.c.Status(ctx)
	if err != nil {
		return nil, err
	}
	results, _ := st["results"].([]any)
	rows := make([]api.Object, 0, len(results))
	for _, r := range results {
		if o, ok := r.(map[string]any); ok {
			rows = append(rows, o)
		}
	}
	return rows, nil
}

// CreateCollection refuses an existing foreign ID instead of returning that
// collection like `oa collection create` does, so the form cannot silently
// show another collection.
func (b browseBackend) CreateCollection(ctx context.Context, data api.Object) (api.Object, error) {
	fid, _ := data["foreign_id"].(string)
	existing, err := b.c.CollectionByForeignID(ctx, fid)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("a collection with foreign ID %q already exists", fid)
	}
	return b.c.CreateCollection(ctx, data)
}

func (b browseBackend) UpdateCollection(ctx context.Context, coll, changes api.Object) (api.Object, error) {
	data := api.Object{}
	for _, k := range editableKeys {
		if v, ok := coll[k]; ok && v != nil {
			data[k] = v
		}
	}
	for k, v := range changes {
		data[k] = v
	}
	return b.c.UpdateCollection(ctx, str(coll, "id"), data, true)
}

// DeleteCollection waits for the deletion, so the list is right afterwards.
func (b browseBackend) DeleteCollection(ctx context.Context, id string) error {
	return b.c.DeleteCollection(ctx, id, true)
}

func (b browseBackend) ReindexCollection(ctx context.Context, id string, flush bool) error {
	return b.c.ReindexCollection(ctx, id, flush, false)
}

func (b browseBackend) ReingestCollection(ctx context.Context, id string, index bool) error {
	return b.c.ReingestCollection(ctx, id, index)
}

// openBrowser opens u in the default web browser. The URL comes from the
// server, so only http and https are passed on to the system opener.
func openBrowser(u string) error {
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", u)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // the opener's exit status is not useful
	return nil
}

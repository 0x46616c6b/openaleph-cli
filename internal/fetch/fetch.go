// Package fetch downloads collections and entities into a folder tree.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

const maxDepth = 64

// Options configures a download.
type Options struct {
	// Prefix is the destination directory (default: the working directory).
	Prefix string
	// Overwrite replaces files that already exist. Without it, existing
	// files are kept: downloads are written to a temporary file first, so an
	// existing file is always a complete one.
	Overwrite bool
	// Log receives progress messages.
	Log func(level, msg string)
	// Progress is called after each file with the counts so far and the
	// name of the file.
	Progress func(r Result, name string)
}

// Result counts what a download did.
type Result struct {
	Fetched int
	Skipped int
}

type fetcher struct {
	c   *api.Client
	o   Options
	res Result
}

func (f *fetcher) logf(format string, args ...any) {
	if f.o.Log != nil {
		f.o.Log("INFO", fmt.Sprintf(format, args...))
	}
}

func destination(prefix string) (string, error) {
	if prefix == "" {
		prefix = "."
	}
	return filepath.Abs(prefix)
}

// Collection downloads every top-level document of a collection.
func Collection(ctx context.Context, c *api.Client, collection api.Object, o Options) (Result, error) {
	dest, err := destination(o.Prefix)
	if err != nil {
		return Result{}, err
	}
	f := &fetcher{c: c, o: o}
	rs, err := c.Search(ctx, api.SearchOptions{
		Filters:  []api.Filter{{Key: "collection_id", Value: api.ID(collection)}},
		Schemata: "Document",
		Params:   url.Values{"empty:properties.parent": {"true"}},
	})
	if err != nil {
		return f.res, err
	}
	label, _ := collection["label"].(string)
	f.logf("Dataset [%s]: %s (%d children)", dest, label, rs.Total())
	for {
		e, ok := rs.Next(ctx)
		if !ok {
			return f.res, rs.Err()
		}
		if err := f.object(ctx, dest, e, 0); err != nil {
			return f.res, err
		}
	}
}

// Entity downloads one entity, recursing into folders.
func Entity(ctx context.Context, c *api.Client, entityID string, o Options) (Result, error) {
	dest, err := destination(o.Prefix)
	if err != nil {
		return Result{}, err
	}
	e, err := c.GetEntity(ctx, entityID, false)
	if err != nil {
		return Result{}, err
	}
	f := &fetcher{c: c, o: o}
	return f.res, f.object(ctx, dest, e, 0)
}

// safeName turns a server-provided name into a single path element, so a
// hostile fileName such as "../../x" cannot escape the destination.
func safeName(name, fallback string) string {
	clean := func(s string) string {
		s = strings.NewReplacer("/", "_", "\\", "_", "\x00", "").Replace(s)
		return strings.TrimSpace(s)
	}
	if n := clean(name); n != "" && n != "." && n != ".." {
		return n
	}
	if n := clean(fallback); n != "" && n != "." && n != ".." {
		return n
	}
	return "unnamed"
}

// fileName picks the longest fileName property, falling back to the entity ID.
func fileName(e api.Object) string {
	id, _ := e["id"].(string)
	props, _ := e["properties"].(map[string]any)
	best := ""
	if names, ok := props["fileName"].([]any); ok {
		for _, n := range names {
			if s, ok := n.(string); ok && len(s) > len(best) {
				best = s
			}
		}
	}
	return safeName(best, id)
}

func (f *fetcher) object(ctx context.Context, dir string, e api.Object, depth int) error {
	if depth > maxDepth {
		return errors.New("folder nesting too deep, giving up")
	}
	name := fileName(e)
	links, _ := e["links"].(map[string]any)
	if fileURL, _ := links["file"].(string); fileURL != "" {
		return f.file(ctx, dir, name, fileURL)
	}

	// A folder: fetch its children.
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return err
	}
	id, _ := e["id"].(string)
	rs, err := f.c.Search(ctx, api.SearchOptions{
		Filters:  []api.Filter{{Key: "properties.parent", Value: id}},
		Schemata: "Document",
	})
	if err != nil {
		return err
	}
	f.logf("Directory [%s]: %s (%d children)", dir, name, rs.Total())
	for {
		child, ok := rs.Next(ctx)
		if !ok {
			return rs.Err()
		}
		if err := f.object(ctx, sub, child, depth+1); err != nil {
			return err
		}
	}
}

func (f *fetcher) file(ctx context.Context, dir, name, fileURL string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); err == nil && !f.o.Overwrite {
		f.logf("Skip [%s]: %s", dir, name)
		f.res.Skipped++
		f.progress(name)
		return nil
	}
	f.logf("Fetch [%s]: %s", dir, name)
	err := f.c.Retry(ctx, func() error { return f.download(ctx, fileURL, dir, target) })
	if err == nil {
		f.res.Fetched++
		f.progress(name)
	}
	return err
}

func (f *fetcher) progress(name string) {
	if f.o.Progress != nil {
		f.o.Progress(f.res, name)
	}
}

// download streams to a temporary file next to target and moves it into
// place when complete.
func (f *fetcher) download(ctx context.Context, fileURL, dir, target string) error {
	resp, err := f.c.Do(ctx, "GET", fileURL, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(dir, ".oa-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("download %s: %w", filepath.Base(target), err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

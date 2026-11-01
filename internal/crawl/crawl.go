package crawl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

// maxFailedLogged limits how many failed paths the final summary lists.
const maxFailedLogged = 50

// Options configures Run.
type Options struct {
	// Root is the directory (or single file) to upload.
	Root string
	// Parallel is the number of concurrent file uploads (at least 1).
	Parallel int
	// Index asks the server to index documents after ingest.
	Index bool
	// Resume continues from an existing state file instead of starting over.
	Resume bool
	// StateFile overrides the default state file location.
	StateFile string
	// Log receives progress messages; level is "INFO", "WARNING" or "ERROR".
	Log func(level, msg string)
}

// Result summarizes a crawl. Counts include earlier sessions when resuming.
type Result struct {
	Uploaded  int
	Failed    int
	StateFile string
}

type job struct {
	path, rel, parentID string
}

type crawler struct {
	c      *api.Client
	collID string
	o      Options
	state  *State
	ignore *Ignorer
	root   string
}

func (cr *crawler) logf(level, format string, args ...any) {
	if cr.o.Log != nil {
		cr.o.Log(level, fmt.Sprintf(format, args...))
	}
}

// Run uploads the contents of o.Root into collection, creating folder
// entities for subdirectories. A single goroutine scans the tree while
// o.Parallel workers upload files. Progress is kept in a SQLite state file
// that is compatible with the Python client.
func Run(ctx context.Context, c *api.Client, collection api.Object, o Options) (Result, error) {
	var res Result
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return res, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return res, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return res, err
	}
	if o.Parallel < 1 {
		o.Parallel = 1
	}

	baseDir := root
	if !info.IsDir() {
		baseDir = filepath.Dir(root)
	}
	statePath := o.StateFile
	if statePath == "" {
		var fallback bool
		if statePath, fallback = StatePath(baseDir); fallback && o.Log != nil {
			o.Log("WARNING", "Cannot write to target directory, using fallback state file: "+statePath)
		}
	}
	state, err := OpenState(statePath, !o.Resume)
	if err != nil {
		return res, err
	}
	defer state.Close()
	res.StateFile = statePath

	cr := &crawler{c: c, collID: api.ID(collection), o: o, state: state, root: root}
	if cr.collID == "" {
		return res, errors.New("collection has no id")
	}
	cr.logf("INFO", "Using state file: %s", statePath)
	if o.Resume {
		cr.logf("INFO", "Resuming crawl from existing state")
	} else {
		cr.logf("INFO", "Starting new crawl")
	}

	if info.IsDir() {
		if cr.ignore, err = LoadIgnorer(root); err != nil {
			return res, err
		}
	} else {
		cr.ignore = NewIgnorer()
	}

	jobs := make(chan job)
	var wg sync.WaitGroup
	for range o.Parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				cr.upload(ctx, j)
			}
		}()
	}

	if info.IsDir() {
		cr.scan(ctx, jobs)
	} else {
		select {
		case jobs <- job{path: root, rel: filepath.Base(root)}:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()

	if res.Uploaded, res.Failed, err = state.Counts(); err != nil {
		return res, err
	}
	if ctx.Err() != nil {
		cr.logf("INFO", "State saved to %s. Exiting.", statePath)
		return res, ctx.Err()
	}

	cr.logf("INFO", "Crawldir complete.")
	cr.logf("INFO", "Uploaded (including prev. sessions if resumed): %d", res.Uploaded)
	cr.logf("INFO", "Failed: %d", res.Failed)
	cr.logf("INFO", "State file location: %s", statePath)
	cr.logf("INFO", "To resume this crawl, use: --resume --state-file %s", statePath)
	if res.Failed > 0 {
		failed, err := state.FailedPaths()
		if err != nil {
			return res, err
		}
		for i, p := range failed {
			if i == maxFailedLogged {
				cr.logf("ERROR", "... and %d more failed paths (see the state file)", len(failed)-maxFailedLogged)
				break
			}
			cr.logf("ERROR", "Failed: %s", p)
		}
	}
	return res, nil
}

// scan walks the tree breadth-first. Folder entities are created here,
// synchronously, because their IDs are the parents of the files below.
func (cr *crawler) scan(ctx context.Context, jobs chan<- job) {
	type dir struct{ path, rel, parentID string }
	queue := []dir{{path: cr.root}}
	visited := map[string]bool{cr.root: true}

	for len(queue) > 0 && ctx.Err() == nil {
		d := queue[0]
		queue = queue[1:]

		parentID := d.parentID
		if d.rel != "" {
			id, err := cr.ingest(ctx, d.path, d.rel, parentID)
			if err != nil {
				if ctx.Err() == nil {
					cr.logf("ERROR", "Failed to create folder %s: %v", d.rel, err)
					_ = cr.state.MarkFailed(d.rel + "/")
				}
				continue // children would lose their parent, retry on resume
			}
			parentID = id
		}

		entries, err := os.ReadDir(d.path)
		if err != nil {
			cr.logf("ERROR", "Cannot read %s: %v", d.path, err)
			continue
		}
		for _, e := range entries {
			child := filepath.Join(d.path, e.Name())
			rel := filepath.ToSlash(strings.TrimPrefix(child, cr.root+string(filepath.Separator)))
			info, err := os.Stat(child) // follows symlinks
			if err != nil {
				cr.logf("WARNING", "Skipping %s: %v", rel, err)
				continue
			}
			if cr.ignore.Ignored(rel, info.IsDir()) {
				continue
			}
			switch {
			case info.IsDir():
				real, err := filepath.EvalSymlinks(child)
				if err != nil || visited[real] {
					cr.logf("WARNING", "Skipping %s: directory loop or unreadable link", rel)
					continue
				}
				visited[real] = true
				queue = append(queue, dir{path: child, rel: rel, parentID: parentID})
			case info.Mode().IsRegular():
				select {
				case jobs <- job{path: child, rel: rel, parentID: parentID}:
				case <-ctx.Done():
					return
				}
			default:
				cr.logf("WARNING", "Skipping %s: not a regular file", rel)
			}
		}
	}
}

// upload sends one file, unless an earlier session already did.
func (cr *crawler) upload(ctx context.Context, j job) {
	if ctx.Err() != nil {
		return
	}
	if done, err := cr.state.Processed(j.rel); err != nil {
		cr.logf("ERROR", "State error for %s: %v", j.rel, err)
		return
	} else if done {
		cr.logf("INFO", "Skipping [%s->%s]: %s", cr.collID, j.parentID, j.rel)
		return
	}
	cr.logf("INFO", "Upload [%s->%s]: %s", cr.collID, j.parentID, j.rel)
	if _, err := cr.ingest(ctx, j.path, j.rel, j.parentID); err != nil {
		if ctx.Err() != nil {
			return // interrupted, not failed: stays pending for --resume
		}
		cr.logf("ERROR", "Failed [%s]: %s: %v", cr.collID, j.rel, err)
		_ = cr.state.MarkFailed(j.rel)
		return
	}
	if err := cr.state.MarkProcessed(j.rel); err != nil {
		cr.logf("ERROR", "State error for %s: %v", j.rel, err)
	}
}

// ingest uploads a file, or creates a folder entity when path is a directory.
func (cr *crawler) ingest(ctx context.Context, path, rel, parentID string) (string, error) {
	meta := api.Object{"foreign_id": rel, "file_name": filepath.Base(path)}
	if parentID != "" {
		meta["parent_id"] = parentID
	}
	filePath := path
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		filePath = ""
	}
	out, err := cr.c.IngestUpload(ctx, cr.collID, filePath, meta, false, cr.o.Index)
	if err != nil {
		return "", err
	}
	id := api.ID(out)
	if id == "" {
		return "", errors.New("upload failed: response has no id")
	}
	return id, nil
}

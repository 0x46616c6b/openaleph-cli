package crawl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0x46616c6b/openaleph-cli/internal/api"
)

type ingestCall struct {
	foreignID string
	parentID  string
	isFile    bool
}

type fakeAleph struct {
	mu     sync.Mutex
	calls  []ingestCall
	failOn map[string]bool
	onFile func(foreignID string)
}

func (f *fakeAleph) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/2/collections/5/ingest" {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseMultipartForm(1 << 20)
	var meta map[string]any
	_ = json.Unmarshal([]byte(r.FormValue("meta")), &meta)
	fid, _ := meta["foreign_id"].(string)
	parent, _ := meta["parent_id"].(string)
	_, _, ferr := r.FormFile("file")
	isFile := ferr == nil

	f.mu.Lock()
	failing := f.failOn[fid]
	if !failing {
		f.calls = append(f.calls, ingestCall{fid, parent, isFile})
	}
	f.mu.Unlock()
	if failing {
		http.Error(w, `{"message":"rejected"}`, http.StatusBadRequest)
		return
	}
	if isFile && f.onFile != nil {
		f.onFile(fid)
	}
	prefix := "dir:"
	if isFile {
		prefix = "file:"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": prefix + fid})
}

func (f *fakeAleph) summary() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		kind := "dir"
		if c.isFile {
			kind = "file"
		}
		out = append(out, kind+" "+c.foreignID+" <- "+c.parentID)
	}
	sort.Strings(out)
	return out
}

func newCrawlClient(t *testing.T, h http.Handler) *api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := api.New(api.Options{
		Host:    srv.URL,
		Retries: 1,
		Sleep:   func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var coll = api.Object{"id": "5"}

func TestRunUploadsTreeWithParents(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"a.txt":            "a",
		"sub/b.txt":        "b",
		"sub/deep/c.txt":   "c",
		"skip.log":         "x",
		"cache/x.txt":      "x",
		".openalephignore": "# comment\n*.log\ncache/\n",
	})
	f := &fakeAleph{}
	res, err := Run(context.Background(), newCrawlClient(t, f), coll, Options{Root: root, Parallel: 3, Index: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"dir sub <- ",
		"dir sub/deep <- dir:sub",
		"file a.txt <- ",
		"file sub/b.txt <- dir:sub",
		"file sub/deep/c.txt <- dir:sub/deep",
	}
	if got := f.summary(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if res.Uploaded != 3 || res.Failed != 0 {
		t.Fatalf("result = %+v", res)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if res.StateFile != filepath.Join(realRoot, ".openaleph_crawl_state.db") {
		t.Fatalf("state file = %s", res.StateFile)
	}
}

func TestRunRefusesToOverwriteState(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.txt": "a"})
	c := newCrawlClient(t, &fakeAleph{})
	if _, err := Run(context.Background(), c, coll, Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), c, coll, Options{Root: root})
	var exists *StateExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResumeSkipsDoneAndRetriesFailed(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c"})

	first := &fakeAleph{failOn: map[string]bool{"b.txt": true}}
	res, err := Run(context.Background(), newCrawlClient(t, first), coll, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 || res.Failed != 1 {
		t.Fatalf("first run = %+v", res)
	}

	second := &fakeAleph{}
	res, err = Run(context.Background(), newCrawlClient(t, second), coll, Options{Root: root, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := second.summary(); len(got) != 1 || got[0] != "file b.txt <- " {
		t.Fatalf("resume must only upload the failed file: %v", got)
	}
	if res.Uploaded != 3 || res.Failed != 0 {
		t.Fatalf("second run = %+v", res)
	}
}

func TestRunInterruptKeepsStateForResume(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &fakeAleph{onFile: func(string) { cancel() }}
	res, err := Run(ctx, newCrawlClient(t, first), coll, Options{Root: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("interrupted uploads must not count as failed: %+v", res)
	}

	second := &fakeAleph{}
	res, err = Run(context.Background(), newCrawlClient(t, second), coll, Options{Root: root, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 3 {
		t.Fatalf("after resume all files must be uploaded: %+v", res)
	}
}

func TestRunSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"report.pdf": "x"})
	f := &fakeAleph{}
	_, err := Run(context.Background(), newCrawlClient(t, f), coll, Options{Root: filepath.Join(root, "report.pdf")})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.summary(); len(got) != 1 || got[0] != "file report.pdf <- " {
		t.Fatalf("calls = %v", got)
	}
}

func TestIgnorer(t *testing.T) {
	ig := NewIgnorer("*.log", "build/", "tmp?", "[!a]x.txt", "docs/*.md")
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"a.log", false, true},
		{"sub/a.log", false, true}, // * also matches /
		{"build", true, true},
		{"build/out.bin", false, false}, // children are only reached through the ignored dir
		{"x/build", true, false},
		{"tmp1", false, true},
		{"tmp12", false, false},
		{"bx.txt", false, true},
		{"ax.txt", false, false},
		{"docs/readme.md", false, true},
		{"readme.md", false, false},
		{".openaleph_crawl_state.db", false, true},
		{".openalephignore", false, true},
		{".openaleph-failed.txt", false, true},
		{"keep.txt", false, false},
	}
	for _, c := range cases {
		if got := ig.Ignored(c.rel, c.isDir); got != c.want {
			t.Errorf("Ignored(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestStateSchemaMatchesPython(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenState(filepath.Join(dir, "s.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.MarkFailed("a"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkProcessed("a"); err != nil {
		t.Fatal(err)
	}
	p, f, _ := st.Counts()
	if p != 1 || f != 0 {
		t.Fatalf("processed=%d failed=%d", p, f)
	}
	if ok, _ := st.Processed("a"); !ok {
		t.Fatal("a must be processed")
	}
	if ok, _ := st.Processed("zzz"); ok {
		t.Fatal("zzz must not be processed")
	}
}

func TestStatePathUsesRootWhenWritable(t *testing.T) {
	root := t.TempDir()
	p, fallback := StatePath(root)
	if fallback || p != filepath.Join(root, ".openaleph_crawl_state.db") {
		t.Fatalf("p=%s fallback=%v", p, fallback)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("the write probe must clean up after itself: %v", entries)
	}
}
